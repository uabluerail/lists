package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/rs/zerolog"

	"github.com/uabluerail/bsky-tools/pagination"
	"github.com/uabluerail/bsky-tools/xrpcauth"
)

var lists = []string{
	"at://did:plc:bmjomljebcsuxolnygfgqtap/app.bsky.graph.list/3jyh6vcbrfl2z", // DNI: � Block ASAP! Report, where applicable!
	"at://did:plc:bmjomljebcsuxolnygfgqtap/app.bsky.graph.list/3jwu5blbqgt27", // DNI: �� russkiy mir
	"at://did:plc:bmjomljebcsuxolnygfgqtap/app.bsky.graph.list/3k2aazxi53l23", // DNI: �� belaruskiy mir or adjacent
	"at://did:plc:bmjomljebcsuxolnygfgqtap/app.bsky.graph.list/3jwuddh3vgc2a", // DNI: �  russkiy mir adjacent
	"at://did:plc:bmjomljebcsuxolnygfgqtap/app.bsky.graph.list/3jwu5sggu2c2w", // DNI: � Hate symbols & Genocide denial
	"at://did:plc:bmjomljebcsuxolnygfgqtap/app.bsky.graph.list/3k3xfqx76nu2y", // DNI: � Mass/Spam followers
	"at://did:plc:bmjomljebcsuxolnygfgqtap/app.bsky.graph.list/3jxnbruulet2p", // DNI: ⛔  Westplainers
	"at://did:plc:bmjomljebcsuxolnygfgqtap/app.bsky.graph.list/3jwv2dafm2c2o", // DNI: � ⚠ disinfo watchlist
	"at://did:plc:bmjomljebcsuxolnygfgqtap/app.bsky.graph.list/3jwuznbtjc42m", // DNI: � � disinfo about Ukraine
	"at://did:plc:bmjomljebcsuxolnygfgqtap/app.bsky.graph.list/3jwu64ervfk2p", // DNI: � Blocked �� community accounts
	"at://did:plc:bmjomljebcsuxolnygfgqtap/app.bsky.graph.list/3jxii7k5wfj2i", // FYI: �� media on �� in English
	"at://did:plc:bmjomljebcsuxolnygfgqtap/app.bsky.graph.list/3kazgj5h6rh25", // FYI: Anonymous accounts
	"at://did:plc:bmjomljebcsuxolnygfgqtap/app.bsky.graph.list/3k44nt5jqug2g", // FYI: � OSINT
	"at://did:plc:bmjomljebcsuxolnygfgqtap/app.bsky.graph.list/3k44n5o5osg2z", // FYI: �� Bots
}

func runMain(ctx context.Context) error {
	ctx = setupLogging(ctx)
	log := zerolog.Ctx(ctx)

	if os.Getenv("BSKY_CREDENTIALS") == "" {
		return fmt.Errorf("BSKY_CREDENTIALS env var needs to be set")
	}

	parts := strings.SplitN(os.Getenv("BSKY_CREDENTIALS"), ":", 2)
	client := xrpcauth.NewClientWithTokenSource(ctx, xrpcauth.PasswordAuth(parts[0], parts[1]))

	for _, uri := range lists {
		fmt.Fprintf(os.Stderr, "::group::%s\n", uri)
		if err := dumpList(ctx, client, uri); err != nil {
			log.Error().Err(err).Msgf("Failed to dump the list %q: %s", uri, err)
		}
		fmt.Fprintf(os.Stderr, "::endgroup::\n")
	}

	return nil
}

func dumpList(ctx context.Context, client *xrpc.Client, uri string) error {
	list, err := pagination.Reduce(func(cursor string) (resp *bsky.GraphGetList_Output, nextCursor string, err error) {
		resp, err = bsky.GraphGetList(ctx, client, cursor, 100, uri)
		if err != nil {
			return
		}
		if resp.Cursor != nil {
			nextCursor = *resp.Cursor
		}
		return
	}, func(resp *bsky.GraphGetList_Output, acc *bsky.GraphGetList_Output) (*bsky.GraphGetList_Output, error) {
		if acc == nil {
			return resp, nil
		}
		acc.Items = append(acc.Items, resp.Items...)
		return acc, nil
	})
	if err != nil {
		return err
	}

	slices.SortFunc(list.Items, func(a, b *bsky.GraphDefs_ListItemView) int {
		if a.Subject == nil || b.Subject == nil {
			return 0
		}
		return cmp.Compare(a.Subject.Did, b.Subject.Did)
	})
	list.Cursor = nil
	// Strip extra info
	for _, item := range list.Items {
		item.Subject.Viewer = nil
		item.Subject.Avatar = nil
		item.Subject.IndexedAt = nil
		item.Subject.Description = nil
		item.Subject.Labels = nil
	}
	list.List.Viewer = nil
	list.List.Creator = nil
	list.List.Avatar = nil

	parts := strings.Split(uri, "/")
	f, err := os.OpenFile(fmt.Sprintf("%s.json", parts[len(parts)-1]), os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to create the output file: %w", err)
	}
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(list); err != nil {
		return fmt.Errorf("marshaling as JSON: %w", err)
	}

	return nil
}

func main() {
	if err := runMain(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func setupLogging(ctx context.Context) context.Context {
	var output io.Writer

	basedir := ""
	_, sourceFile, _, ok := runtime.Caller(0)
	if ok {
		basedir = filepath.Dir(sourceFile) + "/"
	}

	output = zerolog.ConsoleWriter{
		Out:        os.Stderr,
		NoColor:    true,
		TimeFormat: time.RFC3339,
		PartsOrder: []string{
			zerolog.LevelFieldName,
			zerolog.CallerFieldName,
			zerolog.TimestampFieldName,
			zerolog.MessageFieldName,
		},
		FieldsExclude:    []string{"config"},
		FormatFieldName:  func(i interface{}) string { return fmt.Sprintf("%s:", i) },
		FormatFieldValue: func(i interface{}) string { return fmt.Sprintf("%s", i) },
		FormatCaller: func(i interface{}) string {
			s := strings.TrimPrefix(i.(string), basedir)
			if s == "" {
				return "::"
			}
			parts := strings.SplitN(s, ":", 2)
			if len(parts) == 1 {
				return fmt.Sprintf(" filename=%s::", parts[0])
			}
			return fmt.Sprintf(" filename=%s,line=%s::", parts[0], parts[1])
		},
		FormatLevel: func(i interface{}) string {
			if i == nil {
				return "::notice"
			}
			lvl, _ := zerolog.ParseLevel(i.(string))
			ghLevel := "notice"
			switch {
			case lvl <= zerolog.InfoLevel || lvl == zerolog.NoLevel:
				ghLevel = "notice"
			case lvl == zerolog.WarnLevel:
				ghLevel = "warning"
			default:
				ghLevel = "error"
			}
			return fmt.Sprintf("::%s", ghLevel)
		},
	}

	logger := zerolog.New(output).Level(zerolog.Level(zerolog.DebugLevel)).With().Caller().Timestamp().Logger()

	ctx = logger.WithContext(ctx)

	zerolog.DefaultContextLogger = &logger
	log.SetOutput(logger)

	return ctx
}
