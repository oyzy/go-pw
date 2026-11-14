package desktop

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"gokeep/internal/vault"
)

// The dividers keep every pane wide enough to read: these are the least widths of the
// group tree, the entry list and the details, in pixels.
const (
	minTreePane   = 160
	minListPane   = 260
	minDetailPane = 280
)

// splitKey gives a divider the arrow keys while it has the focus: Left and Right move it
// and Home and End take it to its limits. Every other key passes through untouched.
func (m *Model) splitKey(event gfx.KeyEvent) gfx.EventResult {
	if result := m.splitTree.Key(event); result.Handled {
		return result
	}
	return m.splitList.Key(event)
}

// treePane is the group tree on the left. The recycle bin is one of its groups, named
// the way the interface language names it rather than the way the file stores it.
func (m *Model) treePane(c ui.C, width float32) {
	ui.Column(c).Width(max(width, minTreePane)).Background(m.theme.Card).Border(1, m.theme.Border).Children(func() {
		ui.Row(c).FillWidth().Height(28).PadX(10).Items(ui.Center).Children(func() {
			ui.Text(c, m.t("tree.title")).Weight(gfx.WeightSemibold).FontSize(13)
			ui.Spacer(c)
			if ui.Button(c, m.t("toolbar.newGroup")).Ghost().XSmall().Clicked() {
				m.cmdNewGroup()
			}
		})
		// The tree declares only the rows on screen; Node, label and content answer for
		// every visible row of the flattened group list.
		ui.Tree(c, &m.tree, len(m.rows), m.treeName, m.treeContent).Grow(1)
		// The state is read after the declaration, and the indices it reports are the
		// rows of this paint, so they are resolved before the rows are rebuilt.
		toggled, hasToggled := m.tree.Toggled()
		chosen, hasChosen := m.tree.Chosen()
		move, hasMove := m.tree.Dropped()
		if hasChosen && chosen >= 0 && chosen < len(m.rows) {
			m.selectGroup(m.rows[chosen].id)
		}
		if hasToggled && toggled >= 0 && toggled < len(m.rows) {
			row := m.rows[toggled]
			m.expanded[row.id] = !row.expanded
			m.rows = m.buildRows()
		}
		if hasMove {
			m.dropGroup(move)
		}
	})
}

// dropGroup performs the group drag the tree reports.
func (m *Model) dropGroup(move ui.TreeMove) {
	if move.From < 0 || move.From >= len(m.rows) || move.To < 0 || move.To >= len(m.rows) {
		return
	}
	from, to := m.rows[move.From].id, m.rows[move.To].id
	v := m.vault()
	if v == nil || from == "" {
		return
	}
	if err := v.MoveGroup(from, to); err != nil {
		m.fail(err)
	}
	m.reload()
}

// treeRowName is the accessible name of a tree row.
func (m *Model) treeRowName(index int) string {
	if index < 0 || index >= len(m.rows) {
		return ""
	}
	return m.rows[index].name
}

// treeRow declares what is in one tree row: its name, its entry count and the menu a
// secondary press opens.
func (m *Model) treeRow(index int) {
	c := m.pageC
	if index < 0 || index >= len(m.rows) {
		return
	}
	row := m.rows[index]
	// The name is a hit of its own so that the context menu has a target: a plain text
	// is not one (see ui.ContextMenu).
	name := ui.Text(c, row.name).Ellipsis().Hit(ui.ID(c), row.name)
	if row.entries > 0 {
		ui.Text(c, fmt.Sprint(row.entries)).Muted().FontSize(11)
	}
	ui.ContextMenu(c, name, func(items *ui.MenuItems) { m.groupContextMenu(items, row) })
}

// groupContextMenu is the menu of a group row.
func (m *Model) groupContextMenu(items *ui.MenuItems, row groupRow) {
	items.Label(row.name)
	if items.Item(m.t("group.menuNewEntry")).Clicked() {
		m.groupID = row.id
		m.cmdNewEntry()
	}
	if items.Item(m.t("group.menuNewGroup")).Clicked() {
		m.groupID = row.id
		m.cmdNewGroup()
	}
	if row.id != "" {
		items.Separator()
		if items.Item(m.t("group.menuEdit")).Clicked() {
			m.openGroupEditor(row.id)
		}
		if items.Item(m.t("group.menuMove")).Clicked() {
			m.openMoveDialog(false, row.id, row.name)
		}
		if items.Item(m.t("group.menuDelete")).Clicked() {
			m.askDeleteGroup(row.id, row.name)
		}
	}
	if row.recycled {
		items.Separator()
		if items.Item(m.t("group.menuEmptyBin")).Clicked() {
			m.cmdEmptyRecycleBin(false)
		}
	}
}

// listPane is the entry list in the middle.
func (m *Model) listPane(c ui.C, width float32) {
	ui.Column(c).Width(max(width, minListPane)).Border(1, m.theme.Border).Children(func() {
		ui.Row(c).FillWidth().Height(28).PadX(10).Gap(8).Items(ui.Center).Children(func() {
			ui.Text(c, m.listTitle()).Weight(gfx.WeightSemibold).FontSize(13)
			ui.Text(c, m.tf("list.count", "count", len(m.entries))).Muted().FontSize(12)
			ui.Spacer(c)
			if m.recycleView() {
				ui.Text(c, m.t("list.recycleHint")).Muted().FontSize(12)
			}
		})
		ui.List(c, &m.list, len(m.entries), m.listContent).Grow(1)
		if m.list.Changed() {
			if index := m.list.Selected(); index >= 0 && index < len(m.entries) {
				m.selectEntry(m.entries[index].ID)
			}
		}
		if index, ok := m.list.Submitted(); ok && index >= 0 && index < len(m.entries) {
			m.selectEntry(m.entries[index].ID)
			m.openEntryEditor(m.entries[index].ID)
		}
	})
}

// listRow declares one entry row: the columns of the list and its context menu.
func (m *Model) listRow(row ui.ListRow) {
	if row.Index < 0 || row.Index >= len(m.entries) {
		return
	}
	entry := m.entries[row.Index]
	row.Element.PadX(10).PadY(4).Gap(10).Items(ui.Center).
		Hover(ui.Look{}.Background(m.theme.Hover)).
		Children(func() {
			if entry.IsRecycled {
				ui.Text(row.C, m.t("detail.recycledBadge")).Color(m.theme.Danger).FontSize(11)
			}
			cell(row.C, entry.Title, 200)
			cell(row.C, entry.UserName, 130)
			cell(row.C, entry.URL, 170)
			cell(row.C, m.qualityLabel(entry.Quality), 80)
			ui.Text(row.C, shortTime(entry.Modified)).Muted().FontSize(12)
		})
	ui.ContextMenu(row.C, row.Element, func(items *ui.MenuItems) { m.entryContextMenu(items, entry) })
}

// cell is one column of the entry list.
func cell(c ui.C, text string, width float32) {
	ui.Text(c, text).Width(width).Ellipsis()
}

// entryContextMenu is the right-click menu of an entry row.
func (m *Model) entryContextMenu(items *ui.MenuItems, entry vault.EntrySummary) {
	items.Label(entry.Title)
	if items.Item(m.tf("editor.titleEdit", "title", entry.Title)).Clicked() {
		m.selectEntry(entry.ID)
		m.openEntryEditor(entry.ID)
	}
	if items.Item(m.t("menu.duplicateEntry")).Clicked() {
		v := m.vault()
		if v == nil {
			return
		}
		if _, err := v.DuplicateEntry(entry.ID); err != nil {
			m.fail(err)
		}
		m.reload()
	}
	items.Separator()
	if items.Item(m.t("menu.copyUser")).Clicked() {
		m.copyEntryField(entry.ID, "UserName")
	}
	if items.Item(m.t("menu.copyPassword")).Clicked() {
		m.copyEntryField(entry.ID, "Password")
	}
	if items.Item(m.t("menu.copyUrl")).Clicked() {
		m.copyEntryField(entry.ID, "URL")
	}
	if entry.URL != "" {
		if items.Item(m.t("menu.openUrl")).Clicked() {
			m.selectEntry(entry.ID)
			m.cmdOpenURL()
		}
	}
	items.Separator()
	if entry.IsRecycled {
		if items.Item(m.t("group.menuRestore")).Clicked() {
			if v := m.vault(); v != nil {
				if err := v.RestoreToPreviousGroup(entry.ID); err != nil {
					m.fail(err)
				}
			}
			m.reload()
		}
	}
	if items.Item(m.t("group.menuMove")).Clicked() {
		m.openMoveDialog(true, entry.ID, entry.Title)
	}
	items.Separator()
	if items.Item(m.t("menu.delete")).Clicked() {
		m.confirmDeleteEntry(entry.ID, entry.Title, entry.IsRecycled)
	}
}

// detailPane shows the selected entry in full.
func (m *Model) detailPane(c ui.C) {
	ui.Column(c).Grow(1).Background(m.theme.Card).Border(1, m.theme.Border).Children(func() {
		ui.Row(c).FillWidth().Height(28).PadX(10).Items(ui.Center).Children(func() {
			ui.Text(c, m.t("detail.header")).Weight(gfx.WeightSemibold).FontSize(13)
			ui.Spacer(c)
			if ui.Button(c, m.t("detail.edit")).Outline().XSmall().Disabled(!m.hasDetail).Clicked() {
				m.openEntryEditor(m.entryID)
			}
		})
		if !m.hasDetail {
			ui.Column(c).Grow(1).Items(ui.Center).Justify(ui.Center).Children(func() {
				ui.Text(c, m.t("detail.select")).Muted()
			})
			return
		}
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).FillWidth().Pad(12).Gap(8).Children(func() {
				if m.detail.IsRecycled {
					ui.Text(c, m.t("detail.recycledBadge")).Color(m.theme.Danger).FontSize(12)
				}
				if m.detail.IsExpired {
					ui.Text(c, m.t("detail.expiredBadge")).Color(m.theme.Danger).FontSize(12)
				}
				title := m.detail.Title
				if strings.TrimSpace(title) == "" {
					title = m.t("detail.noTitle")
				}
				ui.Text(c, title).FontSize(16).Bold()
				ui.Row(c).Gap(8).Children(func() {
					if ui.Button(c, m.t("detail.copy")).Outline().XSmall().Clicked() {
						m.copyEntryField(m.detail.ID, "Title")
					}
					if ui.Button(c, m.t("detail.edit")).Outline().XSmall().Clicked() {
						m.openEntryEditor(m.detail.ID)
					}
				})
				ui.Divider(c)
				m.detailRow(c, m.t("field.userName"), m.detail.UserName, "UserName")
				m.detailRow(c, m.t("field.password"), passwordMask(m.detail.Password), "Password")
				if m.detail.Password != "" {
					bits, score := estimate(m.detail.Password, m.detail.HasTOTP())
					ui.Text(c, m.tf("detail.qualityText", "score", score, "bits", int(bits))).Muted().FontSize(12)
				}
				m.detailRowURL(c)
				if m.detail.Notes != "" {
					ui.Text(c, m.t("field.notes")).Muted().FontSize(12)
					ui.Paragraph(c, m.detail.Notes, 6)
				}
				if len(m.detail.Tags) > 0 {
					m.detailRow(c, m.t("field.tags"), strings.Join(m.detail.Tags, ", "), "")
				}
				ui.Divider(c)
				m.detailRow(c, m.t("field.group"), m.detail.GroupPath, "")
				m.detailRow(c, m.t("field.created"), m.detail.Created, "")
				m.detailRow(c, m.t("field.modified"), m.detail.Modified, "")
				m.detailRow(c, m.t("field.accessed"), m.detail.Accessed, "")
				if m.detail.Expires {
					m.detailRow(c, m.t("field.expiry"), m.detail.ExpiryTime, "")
				}
				if len(m.detail.Fields) > 0 {
					ui.Divider(c)
					ui.Text(c, m.t("field.customFields")).Muted().FontSize(12)
					for _, field := range m.detail.Fields {
						m.detailRow(c, field.Key, field.Value, "")
					}
				}
				if len(m.detail.Attachments) > 0 {
					ui.Divider(c)
					ui.Text(c, m.t("field.attachments")).Muted().FontSize(12)
					for _, attachment := range m.detail.Attachments {
						m.detailRow(c, attachment.Key, fmt.Sprintf("%d B", attachment.Size), "")
					}
				}
				if len(m.detail.History) > 0 {
					ui.Divider(c)
					ui.Text(c, m.t("field.history")).Muted().FontSize(12)
					for _, item := range m.detail.History {
						m.detailRow(c, item.Modified, item.Title, "")
					}
				}
			})
		})
	})
}

// detailRow shows one labeled value with a copy button when it is worth copying.
func (m *Model) detailRow(c ui.C, label, value, copyField string) {
	if strings.TrimSpace(value) == "" && copyField == "" {
		return
	}
	ui.Row(c).Gap(8).Items(ui.Center).Children(func() {
		ui.Text(c, label).Muted().FontSize(12).Width(90)
		ui.Text(c, value).Grow(1).Ellipsis()
		if copyField != "" {
			if ui.Button(c, m.t("detail.copy")).Ghost().XSmall().Clicked() {
				m.copyEntryField(m.detail.ID, copyField)
			}
		}
	})
}

// detailRowURL shows the entry's URL with the button that opens it.
func (m *Model) detailRowURL(c ui.C) {
	if strings.TrimSpace(m.detail.URL) == "" {
		return
	}
	ui.Row(c).Gap(8).Items(ui.Center).Children(func() {
		ui.Text(c, m.t("field.url")).Muted().FontSize(12).Width(90)
		ui.Text(c, m.detail.URL).Grow(1).Ellipsis()
		if ui.Button(c, m.t("toolbar.openUrl")).Ghost().XSmall().Clicked() {
			m.cmdOpenURL()
		}
		if ui.Button(c, m.t("detail.copy")).Ghost().XSmall().Clicked() {
			m.copyEntryField(m.detail.ID, "URL")
		}
	})
}

// listTitle names what the entry list shows.
func (m *Model) listTitle() string {
	if strings.TrimSpace(m.search) != "" {
		return m.t("search.title")
	}
	if m.groupID == "" {
		return m.t("tree.allEntries")
	}
	for _, row := range m.rows {
		if row.id == m.groupID {
			return row.name
		}
	}
	return m.t("list.title")
}

// recycleView reports whether the entry list shows recycled entries.
func (m *Model) recycleView() bool { return m.includeRecycled() }

// recycleBinID is the identity of the recycle bin group, when the database has one.
func (m *Model) recycleBinID() string {
	v := m.vault()
	if v == nil {
		return ""
	}
	return v.Info().RecycleBinID
}

// passwordMask hides a password of the length that was stored.
func passwordMask(password string) string {
	if password == "" {
		return ""
	}
	return strings.Repeat("\u2022", min(len([]rune(password)), 24))
}

// shortTime trims a stored timestamp to the part the list shows.
func shortTime(value string) string {
	if len(value) >= 16 {
		return strings.ReplaceAll(value[:16], "T", " ")
	}
	return value
}
