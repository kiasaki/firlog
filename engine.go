package firlog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/blevesearch/bleve"
	"github.com/blevesearch/bleve/mapping"
)

type Log struct {
	Id   string
	Time time.Time
	Data map[string]interface{}
}

func (l *Log) FormattedTime() string {
	dt, err := time.Parse(time.RFC3339, l.Data["time"].(string))
	if err != nil {
		panic(err)
	}
	return dt.Format("2006/01/02 15:04:05")
}
func (l *Log) FormattedMessage() string {
	message := l.Data["msg"].(string)
	if level, ok := l.Data["level"]; ok {
		message = level.(string) + " " + message
	}
	return message
}

func (l *Log) FormattedData() string {
	data := map[string]interface{}{}
	for k, v := range l.Data {
		if k == "id" || k == "time" || k == "level" || k == "msg" {
			continue
		}
		data[k] = v
	}
	serialized, err := json.Marshal(data)
	if err != nil {
		panic(err)
	}
	return string(serialized)
}

type Engine struct {
	dataDir string
	indexes map[string]bleve.Index
}

func NewEngine(dataDir string) *Engine {
	engine := &Engine{
		dataDir: dataDir,
		indexes: map[string]bleve.Index{},
	}

	indexesNames, err := listIndexes(dataDir)
	if err != nil {
		panic(err)
	}
	for _, indexName := range indexesNames {
		var err error
		engine.indexes[strings.Split(indexName, "_")[0]], err = bleve.Open(filepath.Join(dataDir, indexName))
		if err != nil {
			panic(err)
		}
	}

	return engine
}

func (e *Engine) Stats() map[string]map[string]interface{} {
	indexesStats := map[string]map[string]interface{}{}
	for date, index := range e.indexes {
		indexesStats[date] = index.StatsMap()
	}
	return indexesStats
}

func (e *Engine) Search(search *bleve.SearchRequest, limit int) ([]*Log, error) {
	logs := []*Log{}

	// TODO extract and cache
	group := bleve.NewIndexAlias()
	for _, index := range e.indexes {
		group.Add(index)
	}

	searchResult, err := group.Search(search)
	if err != nil {
		return nil, err
	}

	for _, hit := range searchResult.Hits {
		dtString := hit.Fields["time"].(string)
		dt, err := time.Parse(time.RFC3339, dtString)
		if err != nil {
			return nil, err
		}

		index, err := e.indexFor(dt.Format("20060102"))
		if err != nil {
			return nil, err
		}

		logValue, err := index.GetInternal([]byte(hit.ID))
		if err != nil {
			return nil, fmt.Errorf("bleve get internal: %v", err)
		}
		log := &Log{Id: hit.ID}
		err = json.Unmarshal(logValue, &log.Data)
		if err != nil {
			return nil, err
		}

		logs = append(logs, log)
	}

	return logs, nil
}

func (e *Engine) Index(logs []*Log) error {
	batches := map[string]*bleve.Batch{}

	for _, log := range logs {
		date := log.Time.Format("20060102")
		index, err := e.indexFor(date)
		if err != nil {
			return err
		}
		batch, ok := batches[date]
		if !ok {
			batches[date] = index.NewBatch()
			batch = batches[date]
		}

		serialized, err := json.Marshal(log.Data)
		if err != nil {
			return err
		}
		batch.Index(log.Id, log.Data)
		batch.SetInternal([]byte(log.Id), serialized)
	}

	for date, batch := range batches {
		index, err := e.indexFor(date)
		if err != nil {
			return err
		}
		err = index.Batch(batch)
		if err != nil {
			return err
		}
	}

	return nil
}

func buildIndexMapping() *mapping.IndexMappingImpl {
	indexMapping := bleve.NewIndexMapping()

	logMapping := bleve.NewDocumentMapping()
	logMapping.AddFieldMappingsAt("time", bleve.NewDateTimeFieldMapping())
	logMapping.AddFieldMappingsAt("level", bleve.NewTextFieldMapping())
	logMapping.AddFieldMappingsAt("msg", bleve.NewTextFieldMapping())

	indexMapping.DefaultMapping = logMapping
	return indexMapping
}

func (e *Engine) indexFor(date string) (bleve.Index, error) {
	if index, ok := e.indexes[date]; ok {
		return index, nil
	}

	var index bleve.Index
	indexPath := filepath.Join(e.dataDir, date+"_1.bleve")
	_, err := os.Stat(indexPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to check existence of index")
	} else if os.IsNotExist(err) {
		index, err = bleve.New(indexPath, buildIndexMapping())
		if err != nil {
			return nil, fmt.Errorf("bleve new: %s", err.Error())
		}
		e.indexes[date] = index
	} else {
		index, err = bleve.Open(indexPath)
		if err != nil {
			return nil, fmt.Errorf("bleve open: %s", err.Error())
		}
		e.indexes[date] = index
	}
	return e.indexes[date], nil
}

func (e *Engine) sortedIndexNames() []string {
	names := []string{}
	for name := range e.indexes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func listIndexes(dataDir string) ([]string, error) {
	d, err := os.Open(dataDir)
	if err != nil {
		return nil, err
	}
	defer d.Close()

	fis, err := d.Readdir(0)
	if err != nil {
		return nil, err
	}

	var names []string
	for _, fi := range fis {
		if !fi.IsDir() || strings.HasPrefix(fi.Name(), ".") {
			continue
		}
		names = append(names, fi.Name())
	}
	sort.Strings(names)
	return names, nil
}
