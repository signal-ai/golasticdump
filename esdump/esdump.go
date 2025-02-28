package esdump

import (
	"context"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
	pb "gopkg.in/cheggaaa/pb.v1"
	elastic "gopkg.in/olivere/elastic.v5"
)

type elasticMessage struct {
	elastic.SearchHit
}

// Options used with ElasticDump
type Options struct {
	Debug              bool
	InputElasticURL    string
	InputElasticSniff  bool
	OutputElasticURL   string
	OutputElasticSniff bool
	ScrollSize         int
	BulkActions        int
	BulkSize           int
	BulkFlushInterval  time.Duration
	Delete             bool
}

// ElasticDump dump elastic data with Options
func ElasticDump(opt Options) (err error) {
	if opt.Debug {
		logger.Level = logrus.DebugLevel
	}

	inputElasticURL, inputElasticIndexName, err := parseElasticURL(opt.InputElasticURL)
	if err != nil {
		return
	}
	inputClient, err := elastic.NewClient(
		elastic.SetURL(inputElasticURL),
		elastic.SetSniff(opt.InputElasticSniff),
	)
	if err != nil {
		return
	}

	outputElasticURL, outputElasticIndexName, err := parseElasticURL(opt.OutputElasticURL)
	if err != nil {
		return
	}
	outputClient, err := elastic.NewClient(
		elastic.SetURL(outputElasticURL),
		elastic.SetSniff(opt.OutputElasticSniff),
	)
	if err != nil {
		return
	}

	ctx := context.Background()
	ctx = contextWithOSSignal(ctx, os.Interrupt, os.Kill)
	g, ctx := errgroup.WithContext(ctx)

	logger.Debug("start")

	totalDoc, err := inputClient.Count(inputElasticIndexName).Do(ctx)
	if err != nil {
		return
	}
	bar := pb.New64(totalDoc).Start()
	defer bar.Finish()

	hits := make(chan elasticMessage, opt.ScrollSize)
	g.Go(func() error {
		defer close(hits)

		// Initialize scroller. Just don't call Do yet.
		scroll := inputClient.Scroll(inputElasticIndexName).Size(opt.ScrollSize)

		return getData(ctx, hits, scroll)
	})

	savedHits := make(chan elasticMessage, opt.ScrollSize)
	g.Go(func() error {
		defer close(savedHits)

		outputBulkProcess, err2 := outputClient.BulkProcessor().
			Name("golasticdump-output").
			BulkActions(opt.BulkActions).
			BulkSize(opt.BulkSize).
			FlushInterval(opt.BulkFlushInterval).
			Do(ctx)
		if err2 != nil {
			return err2
		}

		if err2 := setData(ctx, hits, savedHits, outputBulkProcess, outputElasticIndexName); err2 != nil {
			return err2
		}

		logger.Debug("closing output bulk process")
		return outputBulkProcess.Close()
	})

	g.Go(func() error {
		inputBulkProcess, err2 := inputClient.BulkProcessor().
			Name("golasticdump-input").
			BulkActions(opt.BulkActions).
			BulkSize(opt.BulkSize).
			FlushInterval(opt.BulkFlushInterval).
			Do(ctx)
		if err2 != nil {
			return err2
		}

		if err2 := delData(ctx, savedHits, inputBulkProcess, opt.Delete, bar); err2 != nil {
			return err2
		}

		logger.Debug("closing input bulk process")
		return inputBulkProcess.Close()
	})

	// Check whether any goroutines failed.
	if err = g.Wait(); err != nil {
		return
	}

	return
}

func parseElasticURL(esurl string) (entrypoint string, indexName string, err error) {
	u, err := url.Parse(esurl)
	if err != nil {
		return
	}

	indexName = strings.TrimLeft(u.Path, "/")
	u.Path = ""

	return u.String(), indexName, nil
}

func getData(
	ctx context.Context,
	hits chan<- elasticMessage,
	scroll *elastic.ScrollService,
) (err error) {
	defer func() {
		logger.Debug("getData defer with err: ", err)
	}()
	for {
		results, err := scroll.Do(ctx)
		if err != nil {
			if err == io.EOF {
				return nil // all results retrieved
			}
			return err // something went wrong
		}

		// Send the hits to the hits channel
		for _, hit := range results.Hits.Hits {
			if hit != nil {
				hits <- elasticMessage{*hit}
			}

			// Check if we need to terminate early
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}
	}
}

func setData(
	ctx context.Context,
	hits <-chan elasticMessage,
	savedHits chan<- elasticMessage,
	outputBulkProcess *elastic.BulkProcessor,
	outputElasticIndexName string,
) (err error) {
	defer func() {
		logger.Debug("setData defer with err: ", err)
	}()
	for hit := range hits {
		index := hit.Index
		if outputElasticIndexName != "" {
			index = outputElasticIndexName
		}

		logger.Debugf("setData index:%q type:%q id:%q", index, hit.Type, hit.Id)
		indexRequest := elastic.NewBulkIndexRequest().Index(index).Type(hit.Type).Id(hit.Id).Doc(hit.Source)
		outputBulkProcess.Add(indexRequest)

		// Check if we need to terminate early
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			savedHits <- hit
		}
	}
	logger.Debug("setData finish")
	return nil
}

func delData(
	ctx context.Context,
	savedHits <-chan elasticMessage,
	inputBulkProcess *elastic.BulkProcessor,
	delete bool,
	bar *pb.ProgressBar,
) (err error) {
	defer func() {
		logger.Debug("delData defer with err: ", err)
	}()
	for hit := range savedHits {
		if delete {
			logger.Debugf("delData index:%q type:%q id:%q", hit.Index, hit.Type, hit.Id)
			deleteRequest := elastic.NewBulkDeleteRequest().Index(hit.Index).Type(hit.Type).Id(hit.Id)
			inputBulkProcess.Add(deleteRequest)
		}

		// Check if we need to terminate early
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			bar.Increment()
		}
	}
	logger.Debug("delData finish")
	return nil
}
