package main

import (
  "strconv"
  "net/http"
  "database/sql"
  "github.com/rcrowley/go-metrics"
  "github.com/mopsalarm/go-pr0gramm"
  "encoding/json"
  "time"
)

type CategoryHandler struct {
  database *sql.DB
  timer    metrics.Timer

  handle   func(*sql.DB, pr0gramm.ItemsRequest, *http.Request) (pr0gramm.Items, error)
}

func parseItemRequest(r *http.Request) pr0gramm.ItemsRequest {
  query := pr0gramm.ItemsRequest{Flags: pr0gramm.ContentTypes{pr0gramm.SFW}}

  // parse "older" field
  if formValue := r.FormValue("older"); formValue != "" {
    if value, err := strconv.ParseInt(formValue, 10, 0); err == nil {
      query = query.WithOlderThan(pr0gramm.Id(value))
    }
  }

  // parse "newer" field
  if formValue := r.FormValue("newer"); formValue != "" {
    if value, err := strconv.ParseInt(formValue, 10, 0); err == nil {
      query = query.WithNewerThan(pr0gramm.Id(value))
    }
  }

  // parse "id" field
  if formValue := r.FormValue("id"); formValue != "" {
    if value, err := strconv.ParseInt(formValue, 10, 0); err == nil {
      query = query.WithAround(pr0gramm.Id(value))
    }
  }

  // Add a user
  if formValue := r.FormValue("user"); formValue != "" {
    query = query.WithUser(formValue)
  }

  if formValue := r.FormValue("likes"); formValue != "" {
    query = query.WithLikes(formValue)
  }

  if formValue := r.FormValue("tags"); formValue != "" {
    query = query.WithTag(formValue)
  }

  // parse "flags" field
  if formValue := r.FormValue("flags"); formValue != "" {
    if value, err := strconv.ParseInt(formValue, 10, 0); err != nil {
      query = query.WithFlags(pr0gramm.ToContentTypes(int(value)))
    }
  }

  return query
}

func (ca *CategoryHandler) ServeHTTP(writer http.ResponseWriter, r *http.Request) {
  ca.timer.Time(func() {
    startTime := time.Now()
    itemQuery := parseItemRequest(r)
    items, err := ca.handle(ca.database, itemQuery, r)
    if err != nil {
      writer.WriteHeader(http.StatusInternalServerError)

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

