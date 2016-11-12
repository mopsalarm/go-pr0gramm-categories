package main

import (
	"database/sql"
	"encoding/json"
	"github.com/mopsalarm/go-pr0gramm"
	"github.com/rcrowley/go-metrics"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type SearchProvider func(*sql.DB, pr0gramm.ItemsRequest, url.Values) (*pr0gramm.Items, error)

type CategoryHandler struct {
	database *sql.DB
	timer    metrics.Timer
	handle   SearchProvider
}

func parseItemRequest(r *http.Request) pr0gramm.ItemsRequest {
	query := pr0gramm.ItemsRequest{ContentTypes: pr0gramm.ContentTypes{pr0gramm.SFW}}

	// parse "older" field
	if formValue := r.FormValue("older"); formValue != "" {
		if value, err := strconv.Atoi(formValue); err == nil {
			query = query.WithOlderThan(pr0gramm.Id(value))
		}
	}

	// parse "newer" field
	if formValue := r.FormValue("newer"); formValue != "" {
		if value, err := strconv.Atoi(formValue); err == nil {
			query = query.WithNewerThan(pr0gramm.Id(value))
		}
	}

	// parse "id" field
	if formValue := r.FormValue("id"); formValue != "" {
		if value, err := strconv.Atoi(formValue); err == nil {
			query = query.WithAround(pr0gramm.Id(value))
		}
	}

	// Add a user
	if formValue := r.FormValue("user"); strings.TrimSpace(formValue) != "" {
		query = query.WithUser(strings.TrimSpace(formValue))
	}

	if formValue := r.FormValue("likes"); strings.TrimSpace(formValue) != "" {
		query = query.WithLikes(strings.TrimSpace(formValue))
	}

	if formValue := r.FormValue("tags"); strings.TrimSpace(formValue) != "" {
		query = query.WithTag(strings.TrimSpace(formValue))
	}

	query = query.WithTopOnly(r.FormValue("promoted") == "1")

	// parse "flags" field
	if formValue := r.FormValue("flags"); formValue != "" {
		if value, err := strconv.ParseInt(formValue, 10, 0); err == nil {
			query = query.WithFlags(pr0gramm.ToContentTypes(int(value)))
		}
	}

	return query
}

func (ca *CategoryHandler) ServeHTTP(writer http.ResponseWriter, r *http.Request) {
	ca.timer.Time(func() {
		startTime := time.Now()
		if err := r.ParseForm(); err != nil {
			panic(err)
		}

		itemQuery := parseItemRequest(r)

		items, err := ca.handle(ca.database, itemQuery, r.Form)
		if err != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			log.Println(err)

		} else {
			items.QueryCount = 1
			items.ResponseTime = time.Since(startTime) / time.Millisecond

			result, err := json.Marshal(items)
			if err != nil {
				panic(err)
			}

			writer.Header().Add("Content-Type", "application/json")
			writer.Header().Add("Content-Length", strconv.Itoa(len(result)))
			writer.Write(result)
		}
	})
}
