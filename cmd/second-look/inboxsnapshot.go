package main

import (
	"maps"
	"time"

	"github.com/kyleking/second-look/internal/inbox"
	"github.com/kyleking/second-look/internal/snapshot"
)

// paint fills the buckets from the last snapshot, so the first frame has rows
// and the searches refresh them behind it. A bucket the snapshot does not name
// stays empty: a config that renamed its sections has nothing to paint.
func (s *inboxScreen) paint() {
	if s.snaps == nil {
		return
	}

	snap, age, ok := s.snaps.Inbox()
	if !ok {
		return
	}

	painted := false

	for i := range s.buckets {
		for _, b := range snap.Buckets {
			if b.Name != s.buckets[i].Name {
				continue
			}

			items := snapshot.PullRequests(b.Items)
			s.buckets[i].Items = items
			painted = true

			for key := range inbox.Recall(items, s.ratings, s.local) {
				s.asked[key] = true
			}

			for j := range items {
				s.stale[keyOf(&items[j])] = true
			}
		}
	}

	if !painted {
		return
	}

	maps.Copy(s.records, snap.Facts)
	s.facts = factsOf(s.records)
	s.viewer, s.age = snap.Viewer, age

	if !snap.Verified {
		s.painted = snap.Viewer
	}
}

func factsOf(records map[string]inbox.Record) map[string]inbox.Facts {
	facts := make(map[string]inbox.Facts, len(records))

	for key := range records {
		facts[key] = records[key].Facts
	}

	return facts
}

// absorbRecords takes the facts a read answered. A login other than the one the
// snapshot was read as drops the snapshot, since every fact in it was judged
// for somebody else, and reports that every row has to be read again. A row the
// read could not answer keeps its snapshot facts and stays stale, and a row it
// did not ask about was vouched for by its record.
func (s *inboxScreen) absorbRecords(answered factsMsg) bool {
	redo := false

	if login := answered.fetched.Viewer; login != "" {
		if s.painted != "" && s.painted != login {
			s.snaps.DropInbox()

			s.records = map[string]inbox.Record{}
			s.stale = map[string]bool{}
			redo = true
		}

		s.viewer, s.painted = login, ""
	}

	now := time.Now()

	for i := range s.buckets {
		for j := range s.buckets[i].Items {
			p := &s.buckets[i].Items[j]
			key := keyOf(p)

			if !answered.asked[key] {
				delete(s.stale, key)

				continue
			}

			if f, ok := answered.fetched.Facts[key]; ok {
				s.records[key] = inbox.Record{Facts: f, Updated: p.Updated, Fetched: now}
				delete(s.stale, key)

				continue
			}

			if answered.err == nil {
				delete(s.records, key)
				delete(s.stale, key)
			}
		}
	}

	s.facts = factsOf(s.records)

	return redo
}

// save keeps what the screen holds as the snapshot the next open paints. A
// bucket whose search failed keeps what the last snapshot held for it.
func (s *inboxScreen) save() {
	if s.snaps == nil {
		return
	}

	prev, _, had := s.snaps.Inbox()
	buckets := make([]snapshot.Bucket, 0, len(s.buckets))
	facts := map[string]inbox.Record{}

	for i := range s.buckets {
		b := &s.buckets[i]
		kept := snapshot.Bucket{Name: b.Name, Items: snapshot.RowsOf(b.Items)}

		if b.Err != "" && had {
			for _, was := range prev.Buckets {
				if was.Name == b.Name {
					kept = was
				}
			}
		}

		buckets = append(buckets, kept)

		for j := range b.Items {
			if r, ok := s.records[keyOf(&b.Items[j])]; ok {
				facts[keyOf(&b.Items[j])] = r
			}
		}
	}

	s.snaps.SaveInbox(s.viewer, snapshot.Inbox{
		Buckets: buckets, Facts: facts, Verified: s.viewer != "" && s.painted == "",
	})
}
