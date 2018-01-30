package firlog

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/blevesearch/bleve"
	"github.com/oklog/ulid"
)

var entropyPool sync.Pool

func init() {
	tnano := time.Now().UTC().UnixNano()
	for i := int64(0); i < 10; i++ {
		entropyPool.Put(rand.New(rand.NewSource(tnano + i)))
	}
}

type App struct {
	DataDir string
	Tokens  []string
	Engines map[string]*Engine
}

func NewApp(dataDir string, tokens []string) *App {
	app := &App{
		DataDir: dataDir,
		Tokens:  tokens,
		Engines: map[string]*Engine{},
	}

	for _, token := range tokens {
		app.engineForToken(token)
	}

	return app
}

func (app *App) Start(port, user, pass string) {
	mux := http.NewServeMux()

	staticFilesHandler := http.StripPrefix("/static/", http.FileServer(http.Dir("static")))
	mux.Handle("/static/", staticFilesHandler)
	mux.HandleFunc("/bulk/", app.handleBulk)
	mux.Handle("/stats", basicAuthMiddleware(user, pass)(http.HandlerFunc(app.handleStats)))
	mux.Handle("/", basicAuthMiddleware(user, pass)(http.HandlerFunc(app.handleDashboard)))

	log.Printf("started listening on port %s\n", port)
	log.Fatalln(http.ListenAndServe(":"+port, mux))
}

func (app *App) handleStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	response := map[string]interface{}{}
	for token, engine := range app.Engines {
		response[token] = engine.Stats()
	}
	responseJSON, err := json.Marshal(response)
	if err != nil {
		http.Error(w, "Error serializing response", 500)
		return
	}
	w.Write(responseJSON)
}

func (app *App) handleDashboard(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		token = app.Tokens[0]
	}
	engine := app.engineForToken(token)

	from := r.URL.Query().Get("from")
	if from == "" {
		from = time.Now().UTC().Add(-1 * 24 * time.Hour).Format(time.RFC3339)
	}
	to := r.URL.Query().Get("to")
	if to == "" {
		to = time.Now().UTC().Format(time.RFC3339)
	}

	query := r.URL.Query().Get("query")

	queryWithTime := fmt.Sprintf(`%s time:>="%s" time:<="%s"`, query, from, to)
	fmt.Println("query", queryWithTime)
	search := bleve.NewSearchRequest(bleve.NewQueryStringQuery(queryWithTime))
	search.SortBy([]string{"-time", "-_id"})
	search.Fields = append(search.Fields, "time")
	start := time.Now().UnixNano()
	logs, err := engine.Search(search, 1000)
	searchDuration := float64(time.Now().UnixNano()-start) / 1000000
	if err != nil {
		http.Error(w, "Error executing search", 500)
		return
	}

	t := template.Must(template.New("").Parse(htmlDashboard))
	err = t.Execute(w, map[string]interface{}{
		"query":          query,
		"tokens":         app.Tokens,
		"selectedToken":  token,
		"searchDuration": searchDuration,
		"logsCount":      len(logs),
		"logs":           logs,
	})
	if err != nil {
		log.Println(err)
		w.Write([]byte(err.Error()))
	}
}

func (app *App) handleBulk(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.Write([]byte("only POST supported"))
		return
	}

	token := r.URL.Path[len("/bulk/"):]
	if !contains(app.Tokens, token) {
		w.WriteHeader(401)
		w.Write([]byte("invalid token"))
		return
	}

	body, err := ioutil.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		w.WriteHeader(500)
		w.Write([]byte("error reading body"))
		return
	}

	parsedLogLines := []*Log{}
	logLines := strings.Split(strings.TrimRight(string(body), "\n"), "\n")
	for _, logLine := range logLines {
		// Format:
		// 1 <1>1 2011-11-13T01:11:11+00:00 host app web.1 - message
		logLineParts := strings.SplitN(logLine, " ", 8)
		if len(logLineParts) != 8 {
			log.Printf("malformed line '%s'", logLine)
			continue
		}

		parsedTime, err := time.Parse(time.RFC3339, logLineParts[2])
		if err != nil {
			log.Printf("malformed time '%s'", logLine)
			continue
		}

		message := logLineParts[7]
		data := map[string]interface{}{}
		data["host"] = logLineParts[3]
		data["app"] = logLineParts[4]
		data["process"] = logLineParts[5]
		if message[0] == '{' && message[len(message)-1] == '}' {
			if err := json.Unmarshal([]byte(message), &data); err != nil {
				log.Printf("malformed json '%s'", logLine)
				continue
			}
		} else {
			data["msg"] = message
		}
		id := newUlid()
		data["id"] = id
		data["time"] = parsedTime

		parsedLogLines = append(parsedLogLines, &Log{
			Id:   id,
			Time: parsedTime,
			Data: data,
		})
	}

	if len(parsedLogLines) == 0 {
		w.WriteHeader(200)
		return
	}

	engine := app.engineForToken(token)
	if err := engine.Index(parsedLogLines); err != nil {
		log.Printf("error indexing: %v\n", err)
		w.WriteHeader(500)
		w.Write([]byte("error indexing logs"))
		return
	}
	w.WriteHeader(200)
}

func (app *App) engineForToken(token string) *Engine {
	engine, ok := app.Engines[token]
	if ok {
		return engine
	}
	app.Engines[token] = NewEngine(filepath.Join(app.DataDir, token))
	return app.Engines[token]
}

func contains(values []string, search string) bool {
	for _, value := range values {
		if value == search {
			return true
		}
	}
	return false
}

func newUlid() string {
	entropy := entropyPool.Get().(io.Reader)
	defer entropyPool.Put(entropy)
	return ulid.MustNew(ulid.Timestamp(time.Now().UTC()), entropy).String()
}

const htmlDashboard = `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>firlog</title>
  <link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/bulma/0.6.2/css/bulma.min.css">
  <style>
	.logs {
	  border-bottom: 1px solid #f5f5f5;
	  border-left: 2px solid #f5f5f5;
	  border-right: 2px solid #f5f5f5;
	}
	.logs__header {
	  padding: 0 1rem;
	  border-bottom: 1px solid #f5f5f5;
	}
	.log {
	  font-size: 13px;
	  padding: 0 1rem;
	  border-bottom: 1px solid #f5f5f5;
	  font-family: Menlo, Monaco, Lucida Console, Liberation Mono, DejaVu Sans Mono, Bitstream Vera 
  Sans Mono, Courier New, monospace, serif;
	}
	.log__time { color: hsl(217, 71%, 53%); }
	.log__data { font-weight: bold; }
  </style>
</head>
<body>
  <div class="container">
	<form class="hero is-light is-small">
	  <div class="hero-body columns">
		<div class="column is-3">
		  <div class="field">
			<label class="label">Token</label>
			<div class="control">
			  <div class="select is-fullwidth">
				<select name="token">
				  {{$selectedToken := .selectedToken}}
				  {{range $i, $token := .tokens}}
					<option value="{{$token}}" {{if eq $token $selectedToken}}selected{{else}}{{end}}>{{$token}}</option>
				  {{end}}
				</select>
			  </div>
			</div>
		  </div>
		</div>
		<div class="column">
		  <div class="field">
			<label class="label">Query</label>
			<div class="control">
			  <input class="input" type="text" name="query" placeholder="Query e.g.: 'started -worker port:8001'" value="{{.query}}">
			</div>
		  </div>
		</div>
	  </div>
	</form>
	<div class="logs">
	  <div class="logs__header">
		<strong>{{.logsCount}} results</strong> Took {{.searchDuration | printf "%.2f"}}ms
	  </div>
	  {{range $i, $log := .logs}}
		<div class="log">
		  <span class="log__time">{{$log.FormattedTime}}</span>
		  <span class="log__msg">{{$log.FormattedMessage}}</span>
		  <span class="log__data">{{$log.FormattedData}}</span>
		</div>
	  {{end}}
	</div>
  </div>
</body>
</html>
`
