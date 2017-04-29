package main // import "github.com/mopsalarm/go-pr0gramm-categories"

import (
	"bytes"
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/bobziuchkovski/writ"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	_ "github.com/lib/pq"
	"github.com/mopsalarm/go-pr0gramm"
	"github.com/mopsalarm/pr0gramm-tags/tagsapi"
	"github.com/patrickmn/go-cache"
	"github.com/rcrowley/go-metrics"
	"github.com/vistarmedia/go-datadog"
)

const SAMPLE_PERIOD = time.Minute

type Args struct {
	HelpFlag    bool   `flag:"help" description:"Display this help message and exit"`
	Port        int    `option:"p, port" default:"8080" description:"The port to open the rest service on"`
	Postgres    string `option:"postgres" default:"host=localhost user=postgres password=password sslmode=disable" description:"Postgres DSN for database connection"`
	TagsService string `option:"tags-service" required:"true" description:"Http url of the tags service to use to answer queries."`
	Datadog     string `option:"datadog" description:"Datadog api key for reporting"`
}

var itemCache = cache.New(5*time.Minute, 30*time.Second)

func lookupItemsInCache(itemIds []int32) ([]pr0gramm.Item, []int32) {
	var notFound []int32
	var items []pr0gramm.Item = make([]pr0gramm.Item, 0, len(itemIds))

	for _, itemId := range itemIds {
		key := strconv.Itoa(int(itemId))
		if item, found := itemCache.Get(key); found {
			items = append(items, item.(pr0gramm.Item))
		} else {
			notFound = append(notFound, itemId)
		}
	}

	return items, notFound
}

func putItemsIntoCache(items []pr0gramm.Item) {
	for _, item := range items {
		key := strconv.Itoa(int(item.Id))
		itemCache.Set(key, item, cache.DefaultExpiration)
	}
}

func scanItemsFromCursor(rows *sql.Rows) ([]pr0gramm.Item, error) {
	var items []pr0gramm.Item

	var err error
	if rows != nil {
		for rows.Next() {
			// get the stuff from the cursor
			var created int64
			var item pr0gramm.Item
			err := rows.Scan(&item.Id, &item.Promoted, &item.Up, &item.Down, &item.Flags,
				&item.Image, &item.Source, &item.Thumbnail, &item.Fullsize,
				&item.User, &item.Mark, &created, &item.Width, &item.Height, &item.Audio)

			if err != nil {
				return nil, err
			}

			item.Created = pr0gramm.Timestamp{time.Unix(created, 0)}
			items = append(items, item)
		}

		err = rows.Err()
	}

	return items, err
}

func contentTypesToSearchQuery(types pr0gramm.ContentTypes) string {
	query := []string{}
	for _, contentType := range types {
		switch contentType {
		case pr0gramm.SFW:
			query = append(query, "f:sfw")
		case pr0gramm.NSFW:
			query = append(query, "f:nsfw")
		case pr0gramm.NSFL:
			query = append(query, "f:nsfl")
		case pr0gramm.NSFP:
			query = append(query, "f:nsfp")
		}
	}

	return "(" + strings.Join(query, "|") + ")"
}

func QueryTagsService(db *sql.DB, client *tagsapi.Client, req pr0gramm.ItemsRequest) (*pr0gramm.Items, error) {
	var query []string
	if req.ContentTypes.AsFlags() != pr0gramm.AllContentTypes.AsFlags() {
		// add content type filter
		query = append(query, contentTypesToSearchQuery(req.ContentTypes))
	}

	if req.Tags != "" {
		query = append(query, "("+req.Tags+
			")")
	}

	if req.User != "" {
		query = append(query, "u:"+req.User)
	}

	if req.Top {
		query = append(query, "f:top")
	}

	// req.Older is an items.Promoted id, if req.Top is true.
	if req.Top && req.Older > 0 {
		id, err := resolvePromotedId(db, req.Older)
		if err != nil {
			return nil, fmt.Errorf("Could not resolve promotedId %d: %s", req.Older, err)
		}

		// set the resolved id.
		req.Older = id
	}

	searchConfig := tagsapi.SearchConfig{OlderThan: int(req.Older), Random: req.Random}
	result, err := client.Search(strings.Join(query, "&"), searchConfig)
	if err != nil {
		return nil, err
	}

	// reolve items using memory cache
	items, dbItemIds := lookupItemsInCache(result.Items)

	var rows *sql.Rows
	if len(dbItemIds) > 0 {
		log.WithField("count", len(dbItemIds)).Info("Need to query database for item infos")

		var buffer bytes.Buffer
		for idx, item := range dbItemIds {
			if idx > 0 {
				buffer.WriteRune(',')
			}

			buffer.WriteString(strconv.Itoa(int(item)))
		}

		rows, err = db.Query(`
			SELECT items.id, items.promoted, items.up, items.down, items.flags,
				items.image, items.source, items.thumb, items.fullsize,
				items.username, items.mark, items.created, items.width, items.height, items.audio
			FROM items
			WHERE items.id IN (` + buffer.String() + `)
			LIMIT 120`)

		if err != nil {
			return nil, err
		}

		// cleanup later
		defer rows.Close()

		dbItems, err := scanItemsFromCursor(rows)
		if err != nil {
			return nil, err
		}

		putItemsIntoCache(dbItems)
		items = append(items, dbItems...)
	}

	// sort and filter according to request
	if req.Top {
		items = FilterOnlyPromoted(items)
		sort.Sort(sort.Reverse(TopItemSlice(items)))
	} else {
		sort.Sort(sort.Reverse(NormalItemSlice(items)))
	}

	return &pr0gramm.Items{
		Response: pr0gramm.Response{
			Timestamp: pr0gramm.Timestamp{time.Now()},
		},
		Items:   items,
		AtEnd:   len(items) < 80, // maybe some items got lost?
		AtStart: req.Older == 0,
	}, err
}

func resolvePromotedId(db *sql.DB, promotedId pr0gramm.Id) (id pr0gramm.Id, err error) {
	row := db.QueryRow("SELECT id FROM items WHERE promoted = $1", promotedId)
	err = row.Scan(&id)
	return
}

func parseArguments() *Args {
	args := &Args{}
	cmd := writ.New("categories", args)

	// Parse command line arguments
	_, _, err := cmd.Decode(os.Args[1:])
	if err != nil || args.HelpFlag {
		cmd.ExitHelp(err)
	}

	return args
}

// just send http ok
func ping(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func andTags(previous string, additional string) string {
	if previous != "" {
		return "(" + previous + ")&(" + additional + ")"
	} else {
		return "(" + additional + ")"
	}
}

func main() {
	args := parseArguments()

	// open database connection
	db, err := sql.Open("postgres", args.Postgres)
	if err != nil {
		log.Fatal(err)
		return
	}

	defer db.Close()
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(5 * time.Minute)

	// check if it is valid
	if err = db.Ping(); err != nil {
		log.Fatal(err)
		return
	}

	// get info about the runtime every few seconds
	metrics.RegisterRuntimeMemStats(metrics.DefaultRegistry)
	go metrics.CaptureRuntimeMemStats(metrics.DefaultRegistry, SAMPLE_PERIOD)

	if len(args.Datadog) > 0 {
		host, _ := os.Hostname()

		log.Info("Starting datadog reporter on host %s\n", host)
		go datadog.New(host, args.Datadog).DefaultReporter().Start(SAMPLE_PERIOD)
	}

	httpClient := &http.Client{
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 4,
			IdleConnTimeout:     5 * time.Second,
		},

		Timeout: 10 * time.Second,
	}

	// tag api client
	client, err := tagsapi.NewClient(httpClient, args.TagsService)
	if err != nil {
		panic(err)
	}

	router := mux.NewRouter().StrictSlash(true)
	router.Handle("/general", &CategoryHandler{
		database: db,
		timer:    metrics.GetOrRegisterTimer("pr0gramm.categories.general.query", nil),
		handle: func(db *sql.DB, req pr0gramm.ItemsRequest, urlValues url.Values) (*pr0gramm.Items, error) {
			return QueryTagsService(db, client, req)
		},
	})

	router.Handle("/bestof", &CategoryHandler{
		database: db,
		timer:    metrics.GetOrRegisterTimer("pr0gramm.categories.bestof.query", nil),
		handle: func(db *sql.DB, req pr0gramm.ItemsRequest, urlValues url.Values) (*pr0gramm.Items, error) {
			minScore := 500
			if parsedScore, err := strconv.Atoi(urlValues.Get("score")); err == nil {
				// round down to next multiple of 500
				parsedScore = (parsedScore / 500) * 500
				if parsedScore > 0 {
					minScore = parsedScore
				}
			}

			req.Tags = andTags(req.Tags, fmt.Sprintf("s:%d", minScore))
			return QueryTagsService(db, client, req)
		},
	})

	router.Handle("/text", &CategoryHandler{
		database: db,
		timer:    metrics.GetOrRegisterTimer("pr0gramm.categories.text.query", nil),
		handle: func(db *sql.DB, req pr0gramm.ItemsRequest, urlValues url.Values) (*pr0gramm.Items, error) {
			req.Tags = andTags(req.Tags, "f:text")
			return QueryTagsService(db, client, req)
		},
	})

	router.Handle("/controversial", &CategoryHandler{
		database: db,
		timer:    metrics.GetOrRegisterTimer("pr0gramm.categories.controversial.query", nil),
		handle: func(db *sql.DB, req pr0gramm.ItemsRequest, urlValues url.Values) (*pr0gramm.Items, error) {
			req.Tags = andTags(req.Tags, "f:controversial")
			return QueryTagsService(db, client, req)
		},
	})

	router.Handle("/random", &CategoryHandler{
		database: db,
		timer:    metrics.GetOrRegisterTimer("pr0gramm.categories.controversial.query", nil),
		handle: func(db *sql.DB, req pr0gramm.ItemsRequest, urlValues url.Values) (*pr0gramm.Items, error) {
			return QueryTagsService(db, client, req.WithRandom(true))
		},
	})

	router.HandleFunc("/ping", ping)

	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", args.Port),
		handlers.RecoveryHandler()(
			handlers.LoggingHandler(log.StandardLogger().Writer(),
				handlers.CORS()(router)))))
}
