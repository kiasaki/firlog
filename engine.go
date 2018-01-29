package firlog

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/blevesearch/bleve"
	"github.com/blevesearch/bleve/index"
	"github.com/blevesearch/bleve/mapping"
)

type Log struct {
	Id   string
	Time time.Time
	Data map[string]interface{}
}

type Engine struct {
	dataDir string
	indexes map[string]*bleve.Index
}

func NewEngine(dataDir string) *Engine {
	return &Engine{
		dataDir: dataDir,
		indexes: map[string]*bleve.Index{},
	}
}

func (e *Engine) Index(logs []*Log) error {
	batches := map[string]*index.Batch{}

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

		batch.Index(log.Id, log.Data)
	}

	//index, err := e.indexFor(log)

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

func (e *Engine) indexFor(date string) (*bleve.Index, error) {
	if index, ok := e.indexes[date]; ok {
		return e.indexes[date], nil
	}

	var index *bleve.Index
	indexPath := filepath.Join(e.dataDir, date+"_1.bleve")
	_, err := os.Stat(indexPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to check existence of index")
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
	return e.index[data], nil
}
