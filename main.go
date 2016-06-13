package main // import "github.com/mopsalarm/go-pr0gramm-categories"

import (
	"database/sql"
	"fmt"
	"github.com/bobziuchkovski/writ"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	_ "github.com/lib/pq"
	"github.com/mopsalarm/go-pr0gramm"
	"github.com/rcrowley/go-metrics"
	"github.com/vistarmedia/go-datadog"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const SAMPLE_PERIOD = time.Minute

const AUDIO = 8;

type Args struct {
	HelpFlag bool   `flag:"help" description:"Display this help message and exit"`
	Port     int    `option:"p, port" default:"8080" description:"The port to open the rest service on"`
	Postgres string `option:"postgres" default:"host=localhost user=postgres password=password sslmode=disable" description:"Postgres DSN for database connection"`
	Datadog  string `option:"datadog" description:"Datadog api key for reporting"`
}

// Literal escape the given string.
func EscapeDbString(s string) string {
	p := ""

	if strings.Contains(s, `\`) {
		p = "E"
	}

	s = strings.Replace(s, `'`, `''`, -1)
	s = strings.Replace(s, `\`, `\\`, -1)
	return p + `'` + s + `'`
}

func scanItemsFromCursor(rows *sql.Rows, req pr0gramm.ItemsRequest) pr0gramm.Items {
	itemList := make([]pr0gramm.Item, 0, 120)
	for rows.Next() {
		// get the stuff from the cursor
		var created int64
		var item pr0gramm.Item
		err := rows.Scan(&item.Id, &item.Promoted, &item.Up, &item.Down, &item.Flags,
			&item.Image, &item.Source, &item.Thumbnail, &item.Fullsize,
			&item.User, &item.Mark, &created, &item.Width, &item.Height, &item.Audio)

		if err != nil {
			panic(err)
		}

		item.Created = pr0gramm.Timestamp{time.Unix(created, 0)}
		itemList = append(itemList, item)
	}

	return pr0gramm.Items{
		Response: pr0gramm.Response{
			Timestamp: pr0gramm.Timestamp{time.Now()},
		},
		Items:   itemList,
		AtEnd:   len(itemList) < 120,
		AtStart: req.Older == 0,
	}
}

func HandleControversial(db *sql.DB, req pr0gramm.ItemsRequest, r *http.Request) (pr0gramm.Items, error) {
	rows, err := db.Query(`
    SELECT items.id, items.promoted, items.up, items.down, items.flags,
      items.image, items.source, items.thumb, items.fullsize,
      items.username, items.mark, items.created, items.width, items.height, items.audio
    FROM items
      JOIN controversial ON items.id=controversial.item_id
    WHERE (items.flags & $1 != 0)
      AND ($2 = 0 OR items.id < $2)
      AND ($3 = 0 OR items.id > $3)
      AND items.id NOT IN (
        SELECT tags.item_id FROM tags WHERE tags.item_id=items.id AND tags.confidence>0.3 AND lower(tag)='repost'
    )
    ORDER BY controversial.id DESC LIMIT 120`, req.ContentTypes.AsFlags(), req.Older, req.Newer)

	if err != nil {
		panic(err)
	}

	defer rows.Close()
	return scanItemsFromCursor(rows, req), nil
}

func HandleBestOf(db *sql.DB, req pr0gramm.ItemsRequest, r *http.Request) (pr0gramm.Items, error) {
	var minScore = 2000

	// parse min score value, if present and valid.
	if minScoreStr := r.FormValue("score"); minScoreStr != "" {
		if value, err := strconv.Atoi(minScoreStr); err == nil {
			minScore = value
		}
	}

	// start building the query
	qJoins := []string{"JOIN items ON items_bestof.id=items.id"}
	qWhere := []string{fmt.Sprintf("items_bestof.score > %d", minScore)}

	if req.Tags != nil {
		term := strings.Join(strings.Split(strings.Replace(*req.Tags, "&", "", -1), " "), " & ")
		term = EscapeDbString(term)

		qJoins = append(qJoins, "JOIN tags ON items_bestof.id=tags.item_id")
		qWhere = append(qWhere, fmt.Sprintf(
			"to_tsvector('simple', tags.tag) @@ to_tsquery('simple', %s)", term))
	}

	if req.ContentTypes.AsFlags() != 7 {
		qWhere = append(qWhere, fmt.Sprintf("items.flags & %d != 0", req.ContentTypes.AsFlags()))
	}

	if req.User != nil {
		name := strings.ToLower(*req.User)
		qWhere = append(qWhere, "lower(items.username)=" + EscapeDbString(name))
	}

	query := fmt.Sprintf(`
    SELECT DISTINCT ON (items_bestof.id)
      items.id, items.promoted, items.up, items.down, items.flags,
      items.image, items.source, items.thumb, items.fullsize,
      items.username, items.mark, items.created, items.width, items.height, items.audio
    FROM items_bestof
    %s WHERE %s
      AND ($1 = 0 OR items.id < $1)
      AND ($2 = 0 OR items.id > $2)
    ORDER BY items_bestof.id DESC LIMIT 120
    `,
		strings.Join(qJoins, " "),
		strings.Join(qWhere, " AND "))

	rows, err := db.Query(query, req.Older, req.Newer)
	if err != nil {
		panic(err)
	}

	defer rows.Close()
	return scanItemsFromCursor(rows, req), nil
}

func HandleRandom(db *sql.DB, req pr0gramm.ItemsRequest, r *http.Request) (pr0gramm.Items, error) {
	var rows *sql.Rows
	var err error

	// execute the correct query
	flags := req.ContentTypes.AsFlags()
	if flags == 4 || flags == (4|AUDIO) {
		rows, err = db.Query(QueryRandomNsfl, flags)
	} else {
		rows, err = db.Query(QueryRandomRest, flags)
	}

	if err != nil {
		panic(err)
	}

	defer rows.Close()
	result := scanItemsFromCursor(rows, req)

	// shuffle
	for i := range result.Items {
		j := rand.Intn(i + 1)
		result.Items[i], result.Items[j] = result.Items[j], result.Items[i]
	}

	result.AtEnd = len(result.Items) > 60
	return result, nil
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

func UpdateNsflView(db *sql.DB) {
	go UpdateNsflViewOnce(db)
	for range time.Tick(6 * time.Hour) {
		go UpdateNsflViewOnce(db)
	}
}

func UpdateNsflViewOnce(db *sql.DB) {
	log.Println("Updating nsfl view")

	tx, err := db.Begin()
	if err != nil {
		panic(err)
	}

	_, err = tx.Exec(`
    -- create view with only nsfl posts
    CREATE MATERIALIZED VIEW IF NOT EXISTS random_items_nsfl (id, flags, promoted) AS (
      SELECT id, flags, promoted FROM items WHERE flags=4);

    -- to use "refresh view concurrently", we need a unique index on the id column
    CREATE UNIQUE INDEX IF NOT EXISTS postgres_random_nsfl__id ON random_items_nsfl(id);

    -- refresh the nsfl items.
    REFRESH MATERIALIZED VIEW CONCURRENTLY random_items_nsfl;`)

	if err != nil {
		tx.Rollback()
		panic(err)
	} else {
		log.Println("Updating nsfl view completed")
		tx.Commit()
	}
}

func main() {
	args := parseArguments()

	// open database connection
	db, err := sql.Open("postgres", args.Postgres)
	if err != nil {
		log.Fatal(err)
	}

	defer db.Close()
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(5 * time.Minute)

	// check if it is valid
	if err = db.Ping(); err != nil {
		log.Fatal(err)
	}

	// get info about the runtime every few seconds
	metrics.RegisterRuntimeMemStats(metrics.DefaultRegistry)
	go metrics.CaptureRuntimeMemStats(metrics.DefaultRegistry, SAMPLE_PERIOD)

	if len(args.Datadog) > 0 {
		host, _ := os.Hostname()

		fmt.Printf("Starting datadog reporter on host %s\n", host)
		go datadog.New(host, args.Datadog).DefaultReporter().Start(SAMPLE_PERIOD)
	}

	go UpdateNsflView(db)

	router := mux.NewRouter().StrictSlash(true)

	timer := metrics.NewRegisteredTimer("pr0gramm.categories.controversial.update", nil)
	router.Handle("/controversial", &CategoryHandler{db, timer, HandleControversial})
	router.Handle("/bestof", &CategoryHandler{db, timer, HandleBestOf})
	router.Handle("/random", &CategoryHandler{db, timer, HandleRandom})
	router.HandleFunc("/ping", ping)

	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", args.Port),
		handlers.RecoveryHandler()(
			handlers.LoggingHandler(os.Stdout,
				handlers.CORS()(router)))))
}
