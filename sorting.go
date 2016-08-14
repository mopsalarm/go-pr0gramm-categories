package main

import "github.com/mopsalarm/go-pr0gramm"

type NormalItemSlice []pr0gramm.Item

func (s NormalItemSlice) Len() int {
	return len(s)
}

func (s NormalItemSlice) Less(i, j int) bool {
	return s[i].Id < s[j].Id
}

func (s NormalItemSlice) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

type TopItemSlice []pr0gramm.Item

func (s TopItemSlice) Len() int {
	return len(s)
}

func (s TopItemSlice) Less(i, j int) bool {
	return s[i].Promoted < s[j].Promoted
}

func (s TopItemSlice) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func FilterOnlyPromoted(items []pr0gramm.Item) []pr0gramm.Item {
	target := items[:0]
	for _, item := range items {
		if item.Promoted > 0 {
			target = append(target, item)
		}
	}

	return target
}
