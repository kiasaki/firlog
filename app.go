package firlog

import (
	"encoding/json"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid"
)

var entropyPool sync.Pool

func init() {
	tnano := time.New().UTC().UnixNano()
	for i := 0; i < 10; i++ {
		entropyPool.Put(rand.New(rand.NewSource(tnano + i)))
	}
}

type App struct {
	DataDir string
	Tokens  []string
	Engines map[string]*Engine
}

func NewApp(dataDir string, tokens []string) *App {
	return &App{
		DataDir: dataDir,
		Tokens:  tokens,
		Engines: map[string]*Engine{},
	}
}

func (app *App) Start(port string) {
	mux := http.NewServeMux()

	staticFilesHandler := http.StripPrefix("/static/", http.FileServer(http.Dir("static")))
	mux.Handle("/static/", staticFilesHandler)
	mux.HandleFunc("/bulk/", app.handleBulk)
	mux.HandleFunc("/", app.handleDashboard)

	log.Printf("started listening on port %s\n", port)
	log.Fatalln(http.ListenAndServe(":"+port, mux))
}

func (app *App) handleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(htmlDashboard))
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
		data["time"] = parsedTime

		parsedLogLines = append(parsedLogLines, &Log{
			Id:   newUlid(),
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
</head>
<body>
  firlog
</body>
</html>
`
