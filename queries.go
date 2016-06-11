package main

const QueryRandomNsfl = `
    SELECT
      items.id, items.promoted, items.up, items.down, items.flags,
      items.image, items.source, items.thumb, items.fullsize,
      items.username, items.mark, items.created, items.width, items.height, items.audio
    FROM items
    WHERE id IN (
      (
        SELECT id
        FROM random_items_nsfl TABLESAMPLE BERNOULLI(2)
        WHERE promoted!=0
        ORDER BY random() LIMIT 90)
      UNION (
        SELECT id
        FROM random_items_nsfl TABLESAMPLE BERNOULLI(1)
        WHERE promoted=0
        ORDER BY random() LIMIT 30))`

const QueryRandomRest = `
    (
      SELECT
        items.id, items.promoted, items.up, items.down, items.flags,
        items.image, items.source, items.thumb, items.fullsize,
        items.username, items.mark, items.created, items.width, items.height, items.audio
      FROM items TABLESAMPLE SYSTEM (0.5)
      WHERE flags & $1 != 0 AND promoted != 0
      ORDER BY random() LIMIT 90)
    UNION (
      SELECT
        items.id, items.promoted, items.up, items.down, items.flags,
        items.image, items.source, items.thumb, items.fullsize,
        items.username, items.mark, items.created
      FROM items TABLESAMPLE SYSTEM (0.1)
      WHERE flags & $1 != 0 AND promoted = 0
      ORDER BY random() LIMIT 30)
`
