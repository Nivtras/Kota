package providers

// Antigravity token usage from its local conversation databases.
//
// Each conversation is a SQLite file (~/.gemini/antigravity/conversations/*.db)
// whose rows hold protobuf blobs without field names. The field numbers and
// the de-duplication rules below follow ccusage's Antigravity adapter
// (github.com/ryoppippi/ccusage, rust/adapters/antigravity), which is the
// reference implementation for reading this data.

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (no cgo)
)

var agTokenRoots = []string{
	".gemini/antigravity",
	".gemini/antigravity-cli",
	".gemini/antigravity-ide",
	".gemini/antigravity-backup",
	".gemini/antigravity-acp",
	".config/antigravity",
}

// ---------- minimal protobuf reader ----------

type pbField struct {
	num    uint64
	varint uint64
	bytes  []byte
	isBuf  bool
	isVar  bool
}

var errPB = errors.New("bad protobuf")

func pbVarint(b []byte, i int) (uint64, int, error) {
	var v uint64
	for s := uint(0); s < 64; s += 7 {
		if i >= len(b) {
			return 0, i, errPB
		}
		x := b[i]
		i++
		v |= uint64(x&0x7f) << s
		if x < 0x80 {
			return v, i, nil
		}
	}
	return 0, i, errPB
}

func pbDecode(b []byte) ([]pbField, error) {
	var out []pbField
	for i := 0; i < len(b); {
		key, j, err := pbVarint(b, i)
		if err != nil {
			return nil, err
		}
		i = j
		f := pbField{num: key >> 3}
		switch key & 7 {
		case 0:
			v, j, err := pbVarint(b, i)
			if err != nil {
				return nil, err
			}
			i, f.varint, f.isVar = j, v, true
		case 1:
			i += 8
		case 5:
			i += 4
		case 2:
			l, j, err := pbVarint(b, i)
			if err != nil || j+int(l) > len(b) || int(l) < 0 {
				return nil, errPB
			}
			f.bytes, f.isBuf = b[j:j+int(l)], true
			i = j + int(l)
		default:
			return nil, errPB
		}
		if i > len(b) {
			return nil, errPB
		}
		out = append(out, f)
	}
	return out, nil
}

func pbBytes(fs []pbField, n uint64) []byte {
	for _, f := range fs {
		if f.num == n && f.isBuf {
			return f.bytes
		}
	}
	return nil
}

func pbBytesAll(fs []pbField, n uint64) [][]byte {
	var out [][]byte
	for _, f := range fs {
		if f.num == n && f.isBuf {
			out = append(out, f.bytes)
		}
	}
	return out
}

func pbUint(fs []pbField, n uint64) uint64 {
	for _, f := range fs {
		if f.num == n && f.isVar {
			return f.varint
		}
	}
	return 0
}

func pbText(fs []pbField, n uint64) string {
	if b := pbBytes(fs, n); b != nil {
		return string(b)
	}
	return ""
}

// ---------- usage records ----------

type agModelUsage struct {
	input, totalOut, cacheCreate, cacheRead, reasoning, visibleOut int64
	messageID, responseID, providerMsgID                           string
}

func (m agModelUsage) tokenBearing() bool {
	return m.input > 0 || m.totalOut > 0 || m.cacheCreate > 0 || m.cacheRead > 0 || m.reasoning > 0 || m.visibleOut > 0
}

func (m agModelUsage) identities() []string {
	var ids []string
	if m.responseID != "" {
		ids = append(ids, "response:"+m.responseID)
	}
	if m.providerMsgID != "" {
		ids = append(ids, "provider:"+m.providerMsgID)
	}
	if m.messageID != "" {
		ids = append(ids, "message:"+m.messageID)
	}
	return ids
}

func (m agModelUsage) sameCounts(o agModelUsage) bool {
	return m.input == o.input && m.totalOut == o.totalOut && m.cacheCreate == o.cacheCreate &&
		m.cacheRead == o.cacheRead && m.reasoning == o.reasoning && m.visibleOut == o.visibleOut
}

func parseModelUsage(b []byte) (agModelUsage, error) {
	fs, err := pbDecode(b)
	if err != nil {
		return agModelUsage{}, err
	}
	return agModelUsage{
		input:         int64(pbUint(fs, 2)),
		totalOut:      int64(pbUint(fs, 3)),
		cacheCreate:   int64(pbUint(fs, 4)),
		cacheRead:     int64(pbUint(fs, 5)),
		reasoning:     int64(pbUint(fs, 9)),
		visibleOut:    int64(pbUint(fs, 10)),
		messageID:     pbText(fs, 7),
		responseID:    pbText(fs, 11),
		providerMsgID: pbText(fs, 12),
	}, nil
}

// retries lists every attempt including the primary one; drop that copy.
func parseRetries(blobs [][]byte, primary *agModelUsage) []agModelUsage {
	var out []agModelUsage
	for _, rb := range blobs {
		fs, err := pbDecode(rb)
		if err != nil {
			continue
		}
		if ub := pbBytes(fs, 2); ub != nil {
			if u, err := parseModelUsage(ub); err == nil {
				out = append(out, u)
			}
		}
	}
	if primary != nil {
		pids := strings.Join(primary.identities(), "|")
		for i, r := range out {
			if r.sameCounts(*primary) && strings.Join(r.identities(), "|") == pids {
				out = append(out[:i], out[i+1:]...)
				break
			}
		}
	}
	return out
}

func parseTimestampMsg(b []byte) (time.Time, bool) {
	fs, err := pbDecode(b)
	if err != nil {
		return time.Time{}, false
	}
	sec := int64(pbUint(fs, 1))
	if sec <= 0 {
		return time.Time{}, false
	}
	return time.Unix(sec, int64(min(pbUint(fs, 2), 999_999_999))), true
}

// ---------- events ----------

type agTokenEvent struct {
	ts                                    time.Time
	rank                                  int // 3 own timestamp, 2 via identity, 1 trajectory, 0 file mtime
	input, cacheCreate, cacheRead, outTot int64
	ids                                   []string
}

type agTsRank struct {
	ts   time.Time
	rank int
}

type agEventBuilder struct {
	events     []agTokenEvent
	identityTs map[string]agTsRank
	trajTs     *time.Time
	fileTs     time.Time
}

func (eb *agEventBuilder) add(u agModelUsage, own *time.Time, fallbackIDs []string) {
	if !u.tokenBearing() {
		return
	}
	ids := u.identities()
	if len(ids) == 0 {
		ids = append(ids, fallbackIDs...)
	}
	var tr agTsRank
	switch {
	case own != nil:
		tr = agTsRank{*own, 3}
	default:
		found := false
		for _, id := range ids {
			if v, ok := eb.identityTs[id]; ok {
				tr, found = v, true
				break
			}
		}
		if !found {
			if eb.trajTs != nil {
				tr = agTsRank{*eb.trajTs, 1}
			} else {
				tr = agTsRank{eb.fileTs, 0}
			}
		}
	}
	for _, id := range ids {
		if old, ok := eb.identityTs[id]; !ok || tr.rank > old.rank || (tr.rank == old.rank && tr.ts.Before(old.ts)) {
			eb.identityTs[id] = tr
		}
	}
	eb.events = append(eb.events, agTokenEvent{
		ts: tr.ts, rank: tr.rank,
		input: u.input, cacheCreate: u.cacheCreate, cacheRead: u.cacheRead,
		outTot: max(u.totalOut, u.visibleOut+u.reasoning),
		ids:    ids,
	})
}

func parseAgConversation(path string, fileTs time.Time) ([]agTokenEvent, error) {
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro&_pragma=busy_timeout(2000)")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	tx, err := db.Begin() // one consistent snapshot while Antigravity may be writing
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	session := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	eb := &agEventBuilder{identityTs: map[string]agTsRank{}, fileTs: fileTs}

	if rows, err := tx.Query(`SELECT data FROM trajectory_metadata_blob ORDER BY rowid ASC`); err == nil {
		for rows.Next() && eb.trajTs == nil {
			var blob []byte
			if rows.Scan(&blob) == nil {
				if fs, err := pbDecode(blob); err == nil {
					if tb := pbBytes(fs, 2); tb != nil {
						if t, ok := parseTimestampMsg(tb); ok {
							eb.trajTs = &t
						}
					}
				}
			}
		}
		rows.Close()
	}

	// steps.metadata: usage = 9, retries = 28, timestamp = 8 (or 1)
	if rows, err := tx.Query(`SELECT idx, metadata FROM steps WHERE metadata IS NOT NULL ORDER BY idx ASC`); err == nil {
		for rows.Next() {
			var idx int64
			var blob []byte
			if rows.Scan(&idx, &blob) != nil {
				continue
			}
			fs, err := pbDecode(blob)
			if err != nil {
				continue
			}
			var own *time.Time
			tb := pbBytes(fs, 8)
			if tb == nil {
				tb = pbBytes(fs, 1)
			}
			if tb != nil {
				if t, ok := parseTimestampMsg(tb); ok {
					own = &t
				}
			}
			fallback := []string{"step:" + session + ":" + itoa(idx)}
			var primary *agModelUsage
			if ub := pbBytes(fs, 9); ub != nil {
				if u, err := parseModelUsage(ub); err == nil {
					primary = &u
					eb.add(u, own, fallback)
				}
			}
			for _, r := range parseRetries(pbBytesAll(fs, 28), primary) {
				eb.add(r, own, nil)
			}
		}
		rows.Close()
	}

	// gen_metadata.data: chat model = 1 → usage = 4, retries = 17,
	// generation info = 9 → timestamp = 4; step indices = 2 (root)
	rows, err := tx.Query(`SELECT idx, data FROM gen_metadata ORDER BY idx ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var idx int64
		var blob []byte
		if rows.Scan(&idx, &blob) != nil {
			continue
		}
		root, err := pbDecode(blob)
		if err != nil {
			continue
		}
		cm := pbBytes(root, 1)
		if cm == nil {
			continue
		}
		cfs, err := pbDecode(cm)
		if err != nil {
			continue
		}
		var own *time.Time
		if gi := pbBytes(cfs, 9); gi != nil {
			if gfs, err := pbDecode(gi); err == nil {
				if tb := pbBytes(gfs, 4); tb != nil {
					if t, ok := parseTimestampMsg(tb); ok {
						own = &t
					}
				}
			}
		}
		var fallback []string
		for _, si := range agStepIndices(root) {
			fallback = append(fallback, "step:"+session+":"+itoa(si))
		}
		if len(fallback) == 0 {
			fallback = []string{"gen:" + session + ":" + itoa(idx)}
		}
		var primary *agModelUsage
		if ub := pbBytes(cfs, 4); ub != nil {
			if u, err := parseModelUsage(ub); err == nil {
				primary = &u
				eb.add(u, own, fallback)
			}
		}
		for _, r := range parseRetries(pbBytesAll(cfs, 17), primary) {
			eb.add(r, own, nil)
		}
	}
	return eb.events, nil
}

// agStepIndices reads root field 2, which may be repeated varints or packed.
func agStepIndices(root []pbField) []int64 {
	var out []int64
	for _, f := range root {
		if f.num != 2 {
			continue
		}
		if f.isVar {
			out = append(out, int64(f.varint))
		} else if f.isBuf {
			for i := 0; i < len(f.bytes); {
				v, j, err := pbVarint(f.bytes, i)
				if err != nil {
					break
				}
				out = append(out, int64(v))
				i = j
			}
		}
	}
	return out
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

// dedupAgEvents merges events that share any identity (the same response is
// recorded in both steps and gen_metadata, sometimes across files).
func dedupAgEvents(events []agTokenEvent) []agTokenEvent {
	var slots []*agTokenEvent
	index := map[string]int{}
	for i := range events {
		ev := events[i]
		target := -1
		var others []int
		for _, id := range ev.ids {
			if k, ok := index[id]; ok && slots[k] != nil {
				if target == -1 || k < target {
					if target != -1 {
						others = append(others, target)
					}
					target = k
				} else if k != target {
					others = append(others, k)
				}
			}
		}
		if target == -1 {
			slots = append(slots, &ev)
			for _, id := range ev.ids {
				index[id] = len(slots) - 1
			}
			continue
		}
		for _, k := range others {
			if k != target && slots[k] != nil {
				mergeAgEvent(slots[target], *slots[k])
				slots[k] = nil
			}
		}
		mergeAgEvent(slots[target], ev)
		for _, id := range slots[target].ids {
			index[id] = target
		}
	}
	var out []agTokenEvent
	for _, s := range slots {
		if s != nil {
			out = append(out, *s)
		}
	}
	return out
}

func mergeAgEvent(t *agTokenEvent, d agTokenEvent) {
	t.input = max(t.input, d.input)
	t.cacheCreate = max(t.cacheCreate, d.cacheCreate)
	t.cacheRead = max(t.cacheRead, d.cacheRead)
	t.outTot = max(t.outTot, d.outTot)
	if d.rank > t.rank || (d.rank == t.rank && d.ts.Before(t.ts)) {
		t.ts, t.rank = d.ts, d.rank
	}
	for _, id := range d.ids {
		found := false
		for _, x := range t.ids {
			if x == id {
				found = true
				break
			}
		}
		if !found {
			t.ids = append(t.ids, id)
		}
	}
}

// ---------- aggregation with a per-file cache ----------

type agFileCache struct {
	mod    time.Time
	events []agTokenEvent
}

var (
	agTokCacheMu sync.Mutex
	agTokCache   = map[string]agFileCache{}
)

func agConversationFiles() map[string]time.Time {
	files := map[string]time.Time{}
	home, err := os.UserHomeDir()
	if err != nil {
		return files
	}
	for _, r := range agTokenRoots {
		dir := filepath.Join(home, filepath.FromSlash(r), "conversations")
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".db" {
				continue
			}
			p := filepath.Join(dir, e.Name())
			info, err := e.Info()
			if err != nil {
				continue
			}
			mod := info.ModTime()
			// Writes land in the WAL first; its time is the real "last activity".
			if wi, err := os.Stat(p + "-wal"); err == nil && wi.ModTime().After(mod) {
				mod = wi.ModTime()
			}
			files[p] = mod
		}
	}
	return files
}

func antigravityLocalTokens() []TokenWindow {
	now := time.Now()
	since7 := now.Add(-7 * 24 * time.Hour)
	since5h := now.Add(-5 * time.Hour)

	agTokCacheMu.Lock()
	defer agTokCacheMu.Unlock()

	files := agConversationFiles()
	var all []agTokenEvent
	for p, mod := range files {
		if mod.Before(since7) {
			continue // nothing in this conversation can fall inside the window
		}
		c, ok := agTokCache[p]
		if !ok || !c.mod.Equal(mod) {
			info, _ := os.Stat(p)
			fileTs := mod
			if info != nil {
				fileTs = info.ModTime()
			}
			ev, err := parseAgConversation(p, fileTs)
			if err != nil {
				continue
			}
			c = agFileCache{mod: mod, events: ev}
			agTokCache[p] = c
		}
		all = append(all, c.events...)
	}
	for p := range agTokCache {
		if _, ok := files[p]; !ok {
			delete(agTokCache, p)
		}
	}

	var w5, w7 struct{ in, out int64 }
	for _, e := range dedupAgEvents(all) {
		if e.ts.Before(since7) {
			continue
		}
		in := e.input + e.cacheCreate + e.cacheRead
		w7.in += in
		w7.out += e.outTot
		if !e.ts.Before(since5h) {
			w5.in += in
			w5.out += e.outTot
		}
	}
	return []TokenWindow{
		tokenWindow("Last 5h", w5.in, w5.out),
		tokenWindow("Last 7d", w7.in, w7.out),
	}
}
