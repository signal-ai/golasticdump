package cmd

import (
	"context"
	"time"

	"github.com/spf13/cobra"
	"github.com/tsaikd/KDGoLib/cliutil/cobrather"
	"github.com/tsaikd/KDGoLib/errutil"
	"github.com/tsaikd/golasticdump/esdump"
)

// command line flags
var (
	flagDebug = &cobrather.BoolFlag{
		Name:    "debug",
		Default: false,
		Usage:   "Enable debug logging",
		EnvVar:  "GOESDUMP_DEBUG",
	}
	flagInput = &cobrather.StringFlag{
		Name:   "input",
		Usage:  "Source elastic URL, e.g. http://localhost:9200 or http://localhost:9200/index-*",
		EnvVar: "GOESDUMP_INPUT",
	}
	flagOutput = &cobrather.StringFlag{
		Name:   "output",
		Usage:  "Destination elastic URL, e.g. http://localhost:9200 or http://localhost:9200/copy-index",
		EnvVar: "GOESDUMP_OUTPUT",
	}
	flagScroll = &cobrather.Int64Flag{
		Name:    "scroll",
		Default: 100,
		Usage:   "Load number per operation",
		EnvVar:  "GOESDUMP_SCROLL",
	}
	flagBulkActions = &cobrather.Int64Flag{
		Name:    "bulkactions",
		Default: 0,
		Usage:   "Bulk process document numbers per operation, 0 will use config of scroll",
		EnvVar:  "GOESDUMP_BULK_ACTIONS",
	}
	flagBulkSize = &cobrather.Int64Flag{
		Name:    "bulksize",
		Default: 2,
		Usage:   "Bulk process document size per operation, unit: MB",
		EnvVar:  "GOESDUMP_BULK_SIZE",
	}
	flagBulkFlushInterval = &cobrather.Int64Flag{
		Name:    "bulkflush",
		Default: 30,
		Usage:   "Bulk process flush interval, unit: Second",
		EnvVar:  "GOESDUMP_BULK_FLUSH",
	}
	flagDelete = &cobrather.BoolFlag{
		Name:    "delete",
		Default: false,
		Usage:   "Delete source data after copy",
		EnvVar:  "GOESDUMP_DELETE",
	}
)

// errors
var (
	ErrEmptyConfig1 = errutil.NewFactory("empty config %q")
)

// Module info
var Module = &cobrather.Module{
	Use:   "gogstash",
	Short: "Logstash like, written in golang",
	Commands: []*cobrather.Module{
		cobrather.VersionModule,
	},
	Flags: []cobrather.Flag{
		flagDebug,
		flagInput,
		flagOutput,
		flagScroll,
		flagBulkActions,
		flagBulkSize,
		flagBulkFlushInterval,
		flagDelete,
	},
	RunE: func(ctx context.Context, cmd *cobra.Command, args []string) error {
		inputElasticURL := flagInput.String()
		if inputElasticURL == "" {
			return ErrEmptyConfig1.New(nil, flagInput.Name)
		}
		outputElasticURL := flagOutput.String()
		if outputElasticURL == "" {
			return ErrEmptyConfig1.New(nil, flagOutput.Name)
		}

		scroll := int(flagScroll.Int64())
		bulkActions := int(flagBulkActions.Int64())
		if bulkActions < 1 {
			bulkActions = scroll
		}

		return esdump.ElasticDump(esdump.Options{
			Debug:              flagDebug.Bool(),
			InputElasticURL:    inputElasticURL,
			InputElasticSniff:  false,
			OutputElasticURL:   outputElasticURL,
			OutputElasticSniff: false,
			ScrollSize:         scroll,
			BulkActions:        bulkActions,
			BulkSize:           int(flagBulkSize.Int64()) << 20, // 2 MB
			BulkFlushInterval:  time.Duration(flagBulkFlushInterval.Int64()) * time.Second,
			Delete:             flagDelete.Bool(),
		})
	},
}
