package tui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/aaxis-em/xis/internal/model"
)

// HunkKey identifies a hunk by its file and its lines, not by its position,
// so a review status survives edits elsewhere in the file and is the same
// for the session, --staged and --pr views of one hunk.
func HunkKey(h model.Hunk) string {
	body := h.Patch
	if strings.HasPrefix(body, "@@") {
		if i := strings.IndexByte(body, '\n'); i >= 0 {
			body = body[i+1:]
		}
	}
	sum := sha256.Sum256([]byte(h.FilePath + "\x00" + body))
	return hex.EncodeToString(sum[:8])
}

// key caches HunkKey, which the header needs for every hunk on each frame.
func (m *Model) key(h model.Hunk) string {
	if k, ok := m.keyCache[h.ID]; ok {
		return k
	}
	if m.keyCache == nil {
		m.keyCache = map[string]string{}
	}
	k := HunkKey(h)
	m.keyCache[h.ID] = k
	return k
}

func (m *Model) hunkStatus(h model.Hunk) string {
	if s := m.data.HunkStatus[m.key(h)]; s != "" {
		return s
	}
	return "unreviewed"
}

type progress struct{ understood, questioned, total int }

func (m *Model) progressOf(hunks []model.Hunk) progress {
	p := progress{total: len(hunks)}
	for _, h := range hunks {
		switch m.hunkStatus(h) {
		case "understood":
			p.understood++
		case "questioned":
			p.questioned++
		}
	}
	return p
}

func (p progress) status() string {
	switch {
	case p.questioned > 0:
		return "questioned"
	case p.total > 0 && p.understood == p.total:
		return "understood"
	}
	return "unreviewed"
}

// glyph is ✔ when every hunk is understood, ? when one is questioned, and a
// pie that fills up with the understood hunks in between.
func (p progress) glyph() string {
	switch {
	case p.questioned > 0:
		return "?"
	case p.total == 0 || p.understood == 0:
		return " "
	case p.understood == p.total:
		return "✔"
	case p.understood*8 < p.total*3:
		return "◔"
	case p.understood*8 < p.total*5:
		return "◑"
	}
	return "◕"
}

func (p progress) partial() bool { return p.understood > 0 && p.understood < p.total }

// markHunk sets the status of the current hunk, or clears it when the hunk
// has it already. After "understood" the next hunk is selected.
func (m *Model) markHunk(status string) {
	h, ok := m.curHunk()
	if !ok {
		return
	}
	if m.hunkStatus(h) == status {
		status = "unreviewed"
	}
	if !m.saveHunkStatus([]model.Hunk{h}, status) {
		return
	}
	m.status = fmt.Sprintf("hunk %d of unit %d: %s", m.hunk+1, m.unit+1, status)
	if status == "understood" {
		m.nextHunk()
	}
}

// markUnit marks every visible hunk of the unit understood, or clears them
// when all are understood already.
// On a prompt heading, it marks every hunk of the prompt.
func (m *Model) markUnit() {
	u, ok := m.curUnit()
	if m.onGroup && m.headGroup < len(m.groups) {
		u, ok = Unit{Hunks: m.groupHunks(m.groups[m.headGroup])}, true
	}
	if !ok || len(u.Hunks) == 0 {
		return
	}
	status := "understood"
	if m.progressOf(u.Hunks).status() == "understood" {
		status = "unreviewed"
	}
	if m.saveHunkStatus(u.Hunks, status) {
		m.status = fmt.Sprintf("%d hunks %s", len(u.Hunks), status)
	}
}

func (m *Model) saveHunkStatus(hunks []model.Hunk, status string) bool {
	if m.data.HunkStatus == nil {
		m.data.HunkStatus = map[string]string{}
	}
	for _, h := range hunks {
		k := m.key(h)
		if m.cb.SetHunkStatus != nil {
			if err := m.cb.SetHunkStatus(k, status); err != nil {
				m.status = "save status: " + err.Error()
				return false
			}
		}
		if status == "unreviewed" {
			delete(m.data.HunkStatus, k)
		} else {
			m.data.HunkStatus[k] = status
		}
	}
	m.syncUnitStatus()
	return true
}

// syncUnitStatus stores the status of each unit that follows from its hunks,
// for the report and the note.
func (m *Model) syncUnitStatus() {
	for i := range m.data.Units {
		u := &m.data.Units[i]
		st := m.progressOf(u.Hunks).status()
		if st == u.Status {
			continue
		}
		if m.cb.SetUnitStatus != nil {
			if err := m.cb.SetUnitStatus(u.ID, st); err != nil {
				m.status = "save status: " + err.Error()
				return
			}
		}
		u.Status = st
	}
	for i := range m.units {
		for _, u := range m.data.Units {
			if u.ID == m.units[i].ID {
				m.units[i].Status = u.Status
			}
		}
	}
}

func (m *Model) nextHunk() {
	u, _ := m.curUnit()
	if m.hunk+1 < len(u.Hunks) {
		m.selectHunk(m.hunk + 1)
		return
	}
	if m.unit+1 < len(m.units) {
		m.selectUnit(m.unit + 1)
	}
}
