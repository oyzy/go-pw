package desktop

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"

	"gokeep/internal/vault"
)

// confirmDialog is the question KeePass asks before deleting anything.
func (m *Model) confirmDialog(c ui.C) {
	state := &m.confirm
	ui.ModalWith(c, &state.open, state.title, ui.ModalSpec{Description: state.body, Width: 440}, nil, func() {
		if ui.Button(c, m.t("dialog.cancel")).Outline().Clicked() {
			state.open = false
		}
		label := state.okLabel
		if label == "" {
			label = m.t("dialog.ok")
		}
		button := ui.Button(c, label)
		if state.danger {
			button = button.Destructive()
		}
		if button.Clicked() {
			state.open = false
			if state.action != nil {
				after(c, state.action)
			}
		}
	})
}

// quitDialog asks about unsaved changes when the window is closing.
func (m *Model) quitDialog(c ui.C) {
	if !m.quitAsk {
		return
	}
	allow := m.allowClose
	ui.ModalWith(c, &m.quitAsk, m.t("confirm.unsavedTitle"), ui.ModalSpec{Description: m.t("confirm.unsavedMessage"), Width: 440}, nil, func() {
		if ui.Button(c, m.t("confirm.unsavedSave")).Clicked() {
			if err := m.sess.Save(); err != nil {
				m.fail(err)
				return
			}
			m.finishQuit(allow)
		}
		if ui.Button(c, m.t("confirm.unsavedDiscard")).Outline().Clicked() {
			m.finishQuit(allow)
		}
		if ui.Button(c, m.t("dialog.cancel")).Outline().Clicked() {
			m.quitAsk = false
			m.allowClose = nil
		}
	})
}

// finishQuit closes the database and lets the window close.
func (m *Model) finishQuit(allow func()) {
	m.quitAsk = false
	m.allowClose = nil
	if m.sess.Config().Get().ClipboardClearOnExit {
		m.sess.ClearClipboard()
	}
	m.sess.SetQuitting()
	switch {
	case allow != nil:
		allow()
	case m.window != nil:
		m.window.Close()
	}
}

// editorDialog is the entry editor.
func (m *Model) editorDialog(c ui.C) {
	state := &m.editor
	title := m.t("editor.titleNew")
	if !state.isNew {
		title = m.tf("editor.titleEdit", "title", state.entry.Title)
	}
	ui.ModalWith(c, &state.open, title, ui.ModalSpec{Width: 680, Height: 620}, func() {
		tabs := []string{"general", "fields", "attachments", "autotype", "history"}
		ui.Tabs(c, &state.tab, tabs, m.editorTabLabel)
		ui.Scroll(c).FillWidth().Grow(1).Children(func() {
			ui.Column(c).FillWidth().PadY(8).Gap(8).Children(func() {
				switch state.tab {
				case "fields":
					m.editorFields(c)
				case "attachments":
					m.editorAttachments(c)
				case "autotype":
					m.editorAutoType(c)
				case "history":
					m.editorHistory(c)
				default:
					m.editorGeneral(c)
				}
			})
		})
	}, func() {
		if ui.Button(c, m.t("editor.cancel")).Outline().Clicked() {
			state.open = false
		}
		if ui.Button(c, m.t("editor.save")).Clicked() {
			m.saveEditor()
		}
	})
}

// editorTabLabel names the editor's tabs.
func (m *Model) editorTabLabel(tab string) string {
	switch tab {
	case "fields":
		return m.t("editor.tabFields")
	case "attachments":
		return m.t("editor.tabAttachments")
	case "autotype":
		return m.t("editor.tabAutoType")
	case "history":
		return m.t("editor.tabHistory")
	default:
		return m.t("editor.tabGeneral")
	}
}

// editorGeneral is the first tab of the entry editor. Its fields go two to a row: eight
// captioned fields one per row run past the bottom of the dialog, and the fields below
// the fold are the ones nobody fills in.
func (m *Model) editorGeneral(c ui.C) {
	state := &m.editor
	captionedPair(c,
		func() {
			captioned(c, m.t("field.title"), func() {
				ui.TextField(c, &state.entry.Title, m.t("field.title"), "").AutoFocus()
			})
		},
		func() {
			captioned(c, m.t("field.userName"), func() {
				ui.TextField(c, &state.entry.UserName, m.t("field.userName"), "")
			})
		})
	captionedPair(c,
		func() {
			captionedRow(c, m.t("field.password"), func() {
				if ui.Button(c, m.t("editor.generate")).Outline().XSmall().Clicked() {
					m.openGenerator("editor")
				}
			}, func() {
				m.passwordField(ui.TextField(c, &state.entry.Password, m.t("field.password"), "").Grow(1), &state.passwordShown)
			})
		},
		func() {
			captioned(c, m.t("field.url"), func() {
				ui.TextField(c, &state.entry.URL, m.t("field.url"), "")
			})
		})
	if state.entry.Password != "" {
		_, score := estimate(state.entry.Password, state.entry.HasTOTP())
		ui.Text(c, m.tf("create.strength", "label", m.qualityLabel(score), "score", score)).Muted().FontSize(12)
	}
	captioned(c, m.t("field.notes"), func() {
		ui.TextArea(c, &state.entry.Notes, m.t("field.notes"), "").Height(80)
	})
	captionedPair(c,
		func() {
			captioned(c, m.t("field.overrideUrl"), func() {
				ui.TextField(c, &state.entry.OverrideURL, m.t("field.overrideUrl"), "")
			})
		},
		func() {
			captioned(c, m.t("field.group"), func() {
				ui.TextField(c, &state.entry.GroupPath, m.t("field.group"), "").Disabled(true)
			})
		})
	tags := strings.Join(state.entry.Tags, "; ")
	captionedPair(c,
		func() {
			ui.Column(c).FillWidth().Gap(8).Children(func() {
				ui.Checkbox(c, &state.entry.Expires, m.t("field.expires"))
				if state.entry.Expires {
					captioned(c, m.t("field.expiry"), func() {
						ui.TextField(c, &state.entry.ExpiryTime, m.t("field.expiry"), "2026-01-01T00:00:00Z")
					})
				}
			})
		},
		func() {
			captioned(c, m.t("field.tags"), func() {
				if ui.TextField(c, &tags, m.t("field.tags"), "").Changed() {
					state.entry.Tags = splitTags(tags)
				}
			})
		})
}

// editorFields is the custom-fields tab.
func (m *Model) editorFields(c ui.C) {
	state := &m.editor
	if len(state.entry.Fields) == 0 {
		ui.Text(c, m.t("editor.noFields")).Muted()
	}
	for index := range state.entry.Fields {
		index := index
		field := &state.entry.Fields[index]
		ui.Row(c).Gap(8).Items(ui.End).Children(func() {
			ui.TextField(c, &field.Key, m.t("editor.fieldName"), "").Width(180)
			ui.TextField(c, &field.Value, m.t("editor.fieldValue"), "").Grow(1)
			ui.Checkbox(c, &field.Protected, m.t("editor.protect"))
			if ui.Button(c, m.t("toolbar.delete")).Outline().XSmall().Clicked() {
				state.entry.Fields = append(state.entry.Fields[:index], state.entry.Fields[index+1:]...)
			}
		})
	}
	ui.Divider(c)
	ui.Row(c).Gap(8).Items(ui.End).Children(func() {
		ui.TextField(c, &state.newFieldName, m.t("editor.newFieldName"), "").Width(180)
		ui.TextField(c, &state.newFieldValue, m.t("editor.fieldValue"), "").Grow(1)
		ui.Checkbox(c, &state.newFieldProt, m.t("editor.protect"))
		if ui.Button(c, m.t("editor.addField")).Outline().Clicked() {
			name := strings.TrimSpace(state.newFieldName)
			if name == "" {
				return
			}
			state.entry.Fields = append(state.entry.Fields, vault.FieldDTO{Key: name, Value: state.newFieldValue, Protected: state.newFieldProt})
			state.newFieldName, state.newFieldValue, state.newFieldProt = "", "", false
		}
	})
}

// editorAttachments is the attachments tab.
func (m *Model) editorAttachments(c ui.C) {
	state := &m.editor
	if state.isNew {
		ui.Text(c, m.t("toast.needSaveBeforeAttachment")).Muted()
	}
	if len(state.entry.Attachments) == 0 {
		ui.Text(c, m.t("editor.noAttachments")).Muted()
	}
	for _, attachment := range state.entry.Attachments {
		attachment := attachment
		ui.Row(c).Gap(8).Items(ui.Center).Children(func() {
			ui.Text(c, attachment.Key).Grow(1).Ellipsis()
			ui.Text(c, fmt.Sprintf("%d B", attachment.Size)).Muted().FontSize(12)
			if ui.Button(c, m.t("editor.exportAttachment")).Outline().XSmall().Disabled(state.isNew).Clicked() {
				after(c, func() { m.exportAttachment(state.entry.ID, attachment.Key) })
			}
			if ui.Button(c, m.t("editor.deleteAttachment")).Outline().XSmall().Disabled(state.isNew).Clicked() {
				if err := m.vault().RemoveAttachment(state.entry.ID, attachment.Key); err != nil {
					m.fail(err)
				}
				m.loadDetail()
				m.editor.entry = m.detail
			}
		})
	}
	if ui.Button(c, m.t("editor.addAttachment")).Outline().Disabled(state.isNew).Clicked() {
		after(c, m.addAttachment)
	}
}

// editorAutoType is the auto-type tab.
func (m *Model) editorAutoType(c ui.C) {
	state := &m.editor
	ui.Checkbox(c, &state.entry.AutoTypeEnabled, m.t("editor.autoTypeEnabled"))
	captioned(c, m.t("editor.defaultSequence"), func() {
		ui.TextField(c, &state.entry.AutoTypeSequence, m.t("editor.defaultSequence"), "{USERNAME}{TAB}{PASSWORD}{ENTER}")
	})
	if len(state.entry.AutoTypeItems) == 0 {
		ui.Text(c, m.t("editor.noRules")).Muted()
	}
	for index := range state.entry.AutoTypeItems {
		index := index
		item := &state.entry.AutoTypeItems[index]
		ui.Row(c).Gap(8).Items(ui.End).Children(func() {
			ui.TextField(c, &item.Window, m.t("editor.windowPattern"), "").Grow(1)
			ui.TextField(c, &item.Sequence, m.t("editor.defaultSequence"), "").Grow(1)
			if ui.Button(c, m.t("toolbar.delete")).Outline().XSmall().Clicked() {
				state.entry.AutoTypeItems = append(state.entry.AutoTypeItems[:index], state.entry.AutoTypeItems[index+1:]...)
			}
		})
	}
	if ui.Button(c, m.t("editor.addRule")).Outline().Clicked() {
		state.entry.AutoTypeItems = append(state.entry.AutoTypeItems, vault.AutoTypeAssociationDTO{})
	}
}

// editorHistory is the history tab.
func (m *Model) editorHistory(c ui.C) {
	state := &m.editor
	if len(state.entry.History) == 0 {
		ui.Text(c, m.t("editor.noHistory")).Muted()
		return
	}
	for _, item := range state.entry.History {
		item := item
		ui.Row(c).Gap(8).Items(ui.Center).Children(func() {
			ui.Text(c, item.Modified).Width(160).Ellipsis()
			ui.Text(c, item.Title).Grow(1).Ellipsis()
			if ui.Button(c, m.t("detail.restore")).Outline().XSmall().Disabled(state.isNew).Clicked() {
				index := item.Index
				entryID := state.entry.ID
				m.ask(m.t("detail.historyRestoreTitle"), m.t("detail.historyRestoreConfirm"), m.t("detail.restore"), false, func() {
					if err := m.vault().RestoreHistory(entryID, index); err != nil {
						m.fail(err)
					}
					m.loadDetail()
					m.editor.entry = m.detail
					m.info(m.t("toast.restored"))
				})
			}
		})
	}
}

// saveEditor writes the edited entry back into the database.
func (m *Model) saveEditor() {
	state := &m.editor
	entry := state.entry
	entry.Tags = splitTags(strings.Join(entry.Tags, ";"))
	v := m.vault()
	if v == nil {
		return
	}
	if state.isNew {
		id, err := v.CreateEntry(state.entry.GroupID, entry)
		if err != nil {
			m.fail(err)
			return
		}
		state.open = false
		m.entryID = id
		m.reload()
		m.info(m.t("toast.created"))
		return
	}
	if err := v.UpdateEntry(entry); err != nil {
		m.fail(err)
		return
	}
	state.open = false
	m.reload()
	m.info(m.t("toast.saved"))
}

// groupDialog is the group editor.
func (m *Model) groupDialog(c ui.C) {
	state := &m.group
	title := m.t("group.create")
	if !state.isNew {
		title = m.tf("group.menuEdit")
	}
	ui.ModalWith(c, &state.open, title, ui.ModalSpec{Width: 520}, func() {
		captioned(c, m.t("group.name"), func() {
			ui.TextField(c, &state.name, m.t("group.name"), "").AutoFocus()
		})
		captioned(c, m.t("field.notes"), func() {
			ui.TextArea(c, &state.notes, m.t("field.notes"), "").Height(80)
		})
		captioned(c, m.t("field.tags"), func() {
			ui.TextField(c, &state.tags, m.t("field.tags"), "")
		})
		ui.Row(c).Gap(8).Items(ui.Center).Children(func() {
			ui.Text(c, m.t("group.icon")).Muted().FontSize(12)
			if ui.Button(c, "−").Outline().XSmall().Clicked() {
				state.iconID = clampIcon(state.iconID - 1)
			}
			ui.Text(c, fmt.Sprint(state.iconID)).Width(28)
			if ui.Button(c, "+").Outline().XSmall().Clicked() {
				state.iconID = clampIcon(state.iconID + 1)
			}
		})
		ui.Checkbox(c, &state.expires, m.t("group.expires"))
		if state.expires {
			captioned(c, m.t("field.expiry"), func() {
				ui.TextField(c, &state.expiryTime, m.t("field.expiry"), "2026-01-01T00:00:00Z")
			})
		}
	}, func() {
		if ui.Button(c, m.t("group.cancel")).Outline().Clicked() {
			state.open = false
		}
		if ui.Button(c, m.t("group.save")).Clicked() {
			m.saveGroup()
		}
	})
}

// saveGroup writes the edited group back into the database.
func (m *Model) saveGroup() {
	state := &m.group
	name := strings.TrimSpace(state.name)
	if name == "" {
		state.message = m.t("group.emptyName")
		m.warn(state.message)
		return
	}
	v := m.vault()
	if v == nil {
		return
	}
	dto := vault.GroupDTO{
		ID:         state.id,
		ParentID:   state.parentID,
		Name:       name,
		Notes:      state.notes,
		IconID:     state.iconID,
		Tags:       splitTags(state.tags),
		Expires:    state.expires,
		ExpiryTime: state.expiryTime,
	}
	if state.isNew {
		if _, err := v.CreateGroup(state.parentID, name, state.iconID); err != nil {
			m.fail(err)
			return
		}
	} else if err := v.UpdateGroup(dto); err != nil {
		m.fail(err)
		return
	}
	state.open = false
	m.reload()
	m.info(m.t("toast.saved"))
}

// generatorDialog is the password generator.
func (m *Model) generatorDialog(c ui.C) {
	state := &m.gen
	ui.ModalWith(c, &state.open, m.t("generator.title"), ui.ModalSpec{Width: 560}, func() {
		captioned(c, m.t("field.password"), func() {
			ui.Row(c).FillWidth().Gap(8).Items(ui.End).Children(func() {
				ui.TextField(c, &state.password, m.t("field.password"), "").Grow(1)
				if ui.Button(c, m.t("generator.copy")).Outline().Clicked() {
					if err := m.sess.CopyToClipboard(state.password, true); err != nil {
						m.fail(err)
					}
				}
			})
		})
		bits, score := estimate(state.password, false)
		ui.Text(c, m.tf("generator.qualityLine", "label", m.qualityLabel(score),
			"score", score, "bits", int(bits))).Muted().FontSize(12)
		ui.Row(c).Gap(8).Items(ui.Center).Children(func() {
			ui.Text(c, m.tf("generator.length", "length", state.opts.Length)).Muted().FontSize(12)
			if ui.Button(c, "−").Outline().XSmall().Clicked() {
				state.opts.Length = clampLength(state.opts.Length - 1)
				m.regenerate()
			}
			if ui.Button(c, "+").Outline().XSmall().Clicked() {
				state.opts.Length = clampLength(state.opts.Length + 1)
				m.regenerate()
			}
			if ui.Button(c, m.t("editor.generate")).Outline().XSmall().Clicked() {
				m.regenerate()
			}
		})
		ui.Flow(c).Gap(12).Children(func() {
			ui.Checkbox(c, &state.opts.Upper, m.t("generator.upper"))
			ui.Checkbox(c, &state.opts.Lower, m.t("generator.lower"))
			ui.Checkbox(c, &state.opts.Digits, m.t("generator.digits"))
			ui.Checkbox(c, &state.opts.Special, m.t("generator.special"))
			ui.Checkbox(c, &state.opts.Brackets, m.t("generator.brackets"))
			ui.Checkbox(c, &state.opts.Minus, m.t("generator.minus"))
			ui.Checkbox(c, &state.opts.Under, m.t("generator.under"))
			ui.Checkbox(c, &state.opts.Space, m.t("generator.space"))
			ui.Checkbox(c, &state.opts.Hex, m.t("generator.hex"))
			ui.Checkbox(c, &state.opts.ExcludeLookAlike, m.t("generator.excludeLookAlike"))
			ui.Checkbox(c, &state.opts.NoRepeating, m.t("generator.noRepeating"))
		})
		captioned(c, m.t("generator.customSet"), func() {
			ui.TextField(c, &state.opts.CustomSet, m.t("generator.customSet"), m.t("generator.customSetPlaceholder"))
		})
		ui.Checkbox(c, &state.opts.UsePattern, m.t("generator.usePattern"))
		if state.opts.UsePattern {
			captioned(c, m.t("generator.pattern"), func() {
				ui.TextField(c, &state.opts.Pattern, m.t("generator.pattern"), "")
			})
			ui.Text(c, m.t("generator.patternHint")).Muted().FontSize(12)
		}
	}, func() {
		if ui.Button(c, m.t("generator.saveDefaults")).Outline().Clicked() {
			if err := m.saveGeneratorDefaults(state.opts); err != nil {
				m.fail(err)
			}
			m.info(m.t("toast.generatorDefaults"))
		}
		if ui.Button(c, m.t("generator.use")).Clicked() {
			if m.useGeneratorPassword() {
				state.open = false
			}
		}
		if ui.Button(c, m.t("generator.close")).Outline().Clicked() {
			state.open = false
		}
	})
}

// useGeneratorPassword puts the generated password where the dialog was opened from.
func (m *Model) useGeneratorPassword() bool {
	if m.gen.target == "editor" && m.editor.open {
		m.editor.entry.Password = m.gen.password
		return true
	}
	return false
}

// optionsDialog is the application options dialog.
func (m *Model) optionsDialog(c ui.C) {
	state := &m.opts
	ui.ModalWith(c, &state.open, m.t("options.title"), ui.ModalSpec{Width: 560, Height: 420}, func() {
		ui.Scroll(c).FillWidth().Grow(1).Children(func() {
			ui.Column(c).FillWidth().Gap(10).Children(func() {
				ui.Text(c, m.t("options.clipboard")).Bold()
				ui.Row(c).Gap(8).Items(ui.Center).Children(func() {
					ui.Text(c, m.tf("options.seconds", "seconds", state.cfg.ClipboardClearSeconds)).Muted().FontSize(12)
					if ui.Button(c, "−").Outline().XSmall().Clicked() {
						state.cfg.ClipboardClearSeconds = clampSeconds(state.cfg.ClipboardClearSeconds - 1)
					}
					if ui.Button(c, "+").Outline().XSmall().Clicked() {
						state.cfg.ClipboardClearSeconds = clampSeconds(state.cfg.ClipboardClearSeconds + 1)
					}
				})
				ui.Checkbox(c, &state.cfg.ClipboardClearOnExit, m.t("options.clipboardExit"))
				ui.Divider(c)
				ui.Text(c, m.t("options.autoLock")).Bold()
				ui.Row(c).Gap(8).Items(ui.Center).Children(func() {
					ui.Text(c, m.tf("options.autoLockSeconds", "seconds", state.cfg.AutoLockSeconds)).Muted().FontSize(12)
					if ui.Button(c, "−").Outline().XSmall().Clicked() {
						state.cfg.AutoLockSeconds = clampAutoLockSeconds(state.cfg.AutoLockSeconds - autoLockStep)
					}
					if ui.Button(c, "+").Outline().XSmall().Clicked() {
						state.cfg.AutoLockSeconds = clampAutoLockSeconds(state.cfg.AutoLockSeconds + autoLockStep)
					}
				})
				ui.Checkbox(c, &state.cfg.LockOnMinimize, m.t("options.lockOnMinimize"))
				ui.Checkbox(c, &state.cfg.ConfirmDelete, m.t("options.confirmDelete"))
				ui.Divider(c)
				ui.Text(c, m.t("options.updates")).Bold()
				ui.Checkbox(c, &state.cfg.AutoCheckUpdates, m.t("options.autoCheckUpdates"))
				ui.Text(c, m.t("options.autoCheckUpdatesHint")).Muted().FontSize(12)
				ui.Divider(c)
				ui.Text(c, m.t("options.theme")).Bold()
				themes := []string{"dark", "light"}
				ui.Choice(c, &state.cfg.Theme, themes, m.themeLabel)
				ui.Text(c, m.t("options.language")).Bold()
				languages := make([]string, 0, len(Languages)+1)
				languages = append(languages, "")
				for _, language := range Languages {
					languages = append(languages, language.Code)
				}
				ui.Choice(c, &state.cfg.Language, languages, m.languageLabel)
				ui.Divider(c)
				ui.Text(c, m.t("options.configPath")).Muted().FontSize(12)
				ui.Text(c, m.sess.Config().Path()).Muted().FontSize(12)
			})
		})
	}, func() {
		if ui.Button(c, m.t("options.cancel")).Outline().Clicked() {
			state.open = false
		}
		if ui.Button(c, m.t("options.save")).Clicked() {
			if err := m.saveOptions(); err != nil {
				m.fail(err)
			}
			state.open = false
			m.info(m.t("toast.optionsSaved"))
		}
	})
}

// themeLabel names an appearance for the choice control.
func (m *Model) themeLabel(code string) string {
	if code == "light" {
		return m.t("options.themeLight")
	}
	return m.t("options.themeDark")
}

// languageLabel names a language pack for the choice control.
func (m *Model) languageLabel(code string) string {
	if code == "" {
		return m.t("options.language") + " (auto)"
	}
	for _, language := range Languages {
		if language.Code == code {
			return language.Label
		}
	}
	return code
}

// saveOptions stores the options dialog's settings.
func (m *Model) saveOptions() error {
	cfg := m.opts.cfg
	if err := m.sess.Config().Set(cfg); err != nil {
		return err
	}
	m.sess.Tr().SetLanguage(cfg.Language)
	m.applyTheme()
	m.reload()
	m.sess.ResetAutoLock()
	return nil
}

// settingsDialog is the database settings dialog.
func (m *Model) settingsDialog(c ui.C) {
	state := &m.dbset
	ui.ModalWith(c, &state.open, m.t("settings.title"), ui.ModalSpec{Width: 620, Height: 520}, func() {
		ui.Scroll(c).FillWidth().Grow(1).Children(func() {
			ui.Column(c).FillWidth().Gap(10).Children(func() {
				captioned(c, m.t("settings.name"), func() {
					ui.TextField(c, &state.settings.Name, m.t("settings.name"), "")
				})
				captioned(c, m.t("settings.description"), func() {
					ui.TextField(c, &state.settings.Description, m.t("settings.description"), "")
				})
				captioned(c, m.t("settings.defaultUser"), func() {
					ui.TextField(c, &state.settings.DefaultUserName, m.t("settings.defaultUser"), "")
				})
				ui.Divider(c)
				ui.Text(c, m.t("settings.sectionSecurity")).Bold()
				ui.Text(c, m.tf("settings.format", "value", m.formatName())).Muted().FontSize(12)
				ui.Text(c, m.tf("settings.cipher", "value", state.settings.Cipher)).Muted().FontSize(12)
				ui.Text(c, m.tf("settings.kdf", "value", state.settings.KDFName)).Muted().FontSize(12)
				ui.Text(c, m.tf("settings.memory", "value", state.settings.KDFMemory/1024/1024)).Muted().FontSize(12)
				ui.Text(c, m.tf("settings.iterations", "value", state.settings.KDFIterations)).Muted().FontSize(12)
				ui.Text(c, m.tf("settings.parallelism", "value", state.settings.KDFParallelism)).Muted().FontSize(12)
				ui.Text(c, m.t("settings.kdfHint")).Muted().FontSize(12)
				ui.Divider(c)
				ui.Text(c, m.t("settings.sectionHistory")).Bold()
				ui.Checkbox(c, &state.settings.RecycleBinEnabled, m.t("settings.recycleEnabled"))
				ui.Row(c).Gap(8).Items(ui.Center).Children(func() {
					ui.Text(c, m.tf("settings.historyItems", "value", state.settings.HistoryMaxItems)).Muted().FontSize(12)
					if ui.Button(c, "−").Outline().XSmall().Clicked() {
						state.settings.HistoryMaxItems = max(0, state.settings.HistoryMaxItems-1)
					}
					if ui.Button(c, "+").Outline().XSmall().Clicked() {
						state.settings.HistoryMaxItems = min(200, state.settings.HistoryMaxItems+1)
					}
					ui.Text(c, m.tf("settings.historySize", "value", state.settings.HistoryMaxSize/1024/1024)).Muted().FontSize(12)
					if ui.Button(c, "−").Outline().XSmall().Clicked() {
						state.settings.HistoryMaxSize = max(0, state.settings.HistoryMaxSize-1024*1024)
					}
					if ui.Button(c, "+").Outline().XSmall().Clicked() {
						state.settings.HistoryMaxSize += 1024 * 1024
					}
				})
				ui.Divider(c)
				ui.Text(c, m.t("settings.sectionProtection")).Bold()
				ui.Checkbox(c, &state.settings.ProtectTitle, m.t("settings.protectTitle"))
				ui.Checkbox(c, &state.settings.ProtectUserName, m.t("settings.protectUser"))
				ui.Checkbox(c, &state.settings.ProtectPassword, m.t("settings.protectPassword"))
				ui.Checkbox(c, &state.settings.ProtectURL, m.t("settings.protectUrl"))
				ui.Checkbox(c, &state.settings.ProtectNotes, m.t("settings.protectNotes"))
			})
		})
	}, func() {
		if ui.Button(c, m.t("settings.cancel")).Outline().Clicked() {
			state.open = false
		}
		if ui.Button(c, m.t("settings.save")).Clicked() {
			if err := m.vault().ApplyDatabaseSettings(state.settings); err != nil {
				m.fail(err)
				return
			}
			state.open = false
			m.reload()
			m.info(m.t("toast.settingsUpdated"))
		}
	})
}

// keyDialog changes the master key of the open database.
func (m *Model) keyDialog(c ui.C) {
	state := &m.key
	ui.ModalWith(c, &state.open, m.t("changeKey.title"), ui.ModalSpec{Width: 520}, func() {
		ui.Text(c, m.t("changeKey.hint")).Muted().FontSize(12)
		captioned(c, m.t("changeKey.newPassword"), func() {
			m.passwordField(
				ui.TextField(c, &state.password, m.t("changeKey.newPassword"), "").AutoFocus(),
				&state.showPassword)
		})
		captioned(c, m.t("changeKey.repeat"), func() {
			m.passwordField(ui.TextField(c, &state.repeat, m.t("changeKey.repeat"), ""), &state.showRepeat)
		})
		ui.Checkbox(c, &state.useKeyFile, m.t("changeKey.keyFile"))
		if state.useKeyFile {
			captioned(c, m.t("lock.keyFilePlaceholder"), func() {
				ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
					ui.TextField(c, &state.keyFile, m.t("lock.keyFilePlaceholder"), "").Grow(1)
					if ui.Button(c, m.t("changeKey.choose")).Outline().Clicked() {
						after(c, m.browseKeyFile)
					}
				})
			})
		}
	}, func() {
		if ui.Button(c, m.t("changeKey.cancel")).Outline().Clicked() {
			state.open = false
		}
		if ui.Button(c, m.t("changeKey.apply")).Clicked() {
			if state.password != state.repeat {
				m.warn(m.t("changeKey.mismatch"))
				return
			}
			keyFile := ""
			if state.useKeyFile {
				keyFile = state.keyFile
			}
			if err := m.sess.ChangeKey(state.password, keyFile); err != nil {
				m.fail(err)
				return
			}
			state.open = false
			m.reload()
			m.info(m.t("toast.keyChanged"))
		}
	})
}

// moveDialog moves an entry or a group to another group.
func (m *Model) moveDialog(c ui.C) {
	state := &m.move
	choices := m.groupChoices()
	ui.ModalWith(c, &state.open, m.t("move.title"), ui.ModalSpec{Description: m.tf("move.action", "name", state.label), Width: 420}, func() {
		if len(choices) == 0 {
			ui.Text(c, m.t("group.emptyName")).Muted()
			return
		}
		ui.Select(c, &state.target, choices, m.groupChoiceLabel)
	}, func() {
		if ui.Button(c, m.t("dialog.cancel")).Outline().Clicked() {
			state.open = false
		}
		if ui.Button(c, m.t("dialog.ok")).Clicked() {
			m.applyMove()
		}
	})
}

// applyMove performs the move the dialog asked for.
func (m *Model) applyMove() {
	state := &m.move
	v := m.vault()
	if v == nil || state.target == "" {
		state.open = false
		return
	}
	var err error
	if state.entry {
		err = v.MoveEntry(state.id, state.target)
	} else {
		err = v.MoveGroup(state.id, state.target)
	}
	if err != nil {
		m.fail(err)
		return
	}
	state.open = false
	m.reload()
	m.info(m.t("toast.saved"))
}

// groupChoices lists the groups an item can be moved into.
func (m *Model) groupChoices() []string {
	choices := []string{}
	for _, row := range m.rows {
		if row.id == "" {
			continue
		}
		if !m.move.entry && row.id == m.move.id {
			continue
		}
		choices = append(choices, row.id)
	}
	if m.move.entry {
		choices = append([]string{""}, choices...)
	}
	if m.move.target == "" && len(choices) > 0 {
		m.move.target = choices[0]
	}
	return choices
}

// groupChoiceLabel names a group in the move dialog.
func (m *Model) groupChoiceLabel(id string) string {
	if id == "" {
		return m.t("group.rootLabel")
	}
	for _, row := range m.rows {
		if row.id == id {
			return strings.Repeat("    ", row.depth) + row.name
		}
	}
	if v := m.vault(); v != nil {
		if info := v.Info(); info.RecycleBinID == id {
			return m.t("app.recycleBin")
		}
	}
	return id
}

// searchDialog is the search options dialog.
func (m *Model) searchDialog(c ui.C) {
	open := &m.searchOpen
	ui.ModalWith(c, open, m.t("search.title"), ui.ModalSpec{Width: 460}, func() {
		captioned(c, m.t("search.query"), func() {
			ui.TextField(c, &m.search, m.t("search.query"), "").AutoFocus()
		})
		ui.Checkbox(c, &m.searchCase, m.t("search.caseSensitive"))
		ui.Checkbox(c, &m.searchRegex, m.t("search.regex"))
		ui.Checkbox(c, &m.searchAll, m.t("search.allGroups"))
		ui.Checkbox(c, &m.searchProt, m.t("search.includeProtected"))
	}, func() {
		if ui.Button(c, m.t("search.cancel")).Outline().Clicked() {
			m.search = ""
			*open = false
			m.reloadEntries()
		}
		if ui.Button(c, m.t("search.submit")).Clicked() {
			*open = false
			m.reloadEntries()
		}
	})
}

// aboutDialog describes the application and the format it writes.
func (m *Model) aboutDialog(c ui.C) {
	ui.ModalWith(c, &m.about, m.t("about.title"), ui.ModalSpec{Width: 560}, func() {
		ui.Text(c, m.product).Bold()
		ui.Text(c, m.t("about.intro")).Muted().FontSize(12)
		ui.Divider(c)
		ui.Text(c, m.t("about.format")).Bold()
		ui.Text(c, m.tf("about.formatText", "ext", databaseExtension)).Muted().FontSize(12)
		ui.Text(c, m.t("about.cipher")).Bold()
		ui.Text(c, m.t("about.cipherText")).Muted().FontSize(12)
		ui.Text(c, m.t("about.kdf")).Bold()
		ui.Text(c, m.t("about.kdfText")).Muted().FontSize(12)
		ui.Text(c, m.t("about.keyFiles")).Bold()
		ui.Text(c, m.t("about.keyFilesText")).Muted().FontSize(12)
	}, func() {
		if ui.Button(c, m.t("about.close")).Outline().Clicked() {
			m.about = false
		}
	})
}

// shortcutsDialog lists the keystrokes the application handles. It is built from
// shortcutRows, so it always matches the shortcuts the page declares.
func (m *Model) shortcutsDialog(c ui.C) {
	ui.ModalWith(c, &m.shortcutsHelp, m.t("shortcuts.title"), ui.ModalSpec{Width: 520, Height: 460}, func() {
		ui.Text(c, m.t("shortcuts.hint")).Muted().FontSize(12)
		ui.Divider(c)
		ui.Scroll(c).FillWidth().Grow(1).Children(func() {
			ui.Column(c).FillWidth().Gap(6).Children(func() {
				for _, row := range shortcutRows {
					row := row
					ui.Row(c).FillWidth().Gap(12).Items(ui.Center).Children(func() {
						ui.Text(c, row.keys).Width(150).Bold()
						ui.Text(c, m.t(row.desc)).Grow(1)
					})
				}
			})
		})
	}, func() {
		if ui.Button(c, m.t("about.close")).Outline().Clicked() {
			m.shortcutsHelp = false
		}
	})
}

// duplicatesDialog lists entries that share a password.
func (m *Model) duplicatesDialog(c ui.C) {
	open := len(m.dups) > 0
	ui.ModalWith(c, &open, m.t("duplicates.title"), ui.ModalSpec{Width: 560, Height: 420}, func() {
		if len(m.dups) == 0 {
			ui.Text(c, m.t("duplicates.none")).Muted()
			return
		}
		ui.Scroll(c).FillWidth().Grow(1).Children(func() {
			ui.Column(c).FillWidth().Gap(8).Children(func() {
				for index, group := range m.dups {
					ui.Text(c, m.tf("duplicates.group", "index", index+1, "count", len(group))).Bold()
					for _, entry := range group {
						entry := entry
						ui.Row(c).Gap(8).Items(ui.Center).Children(func() {
							ui.Text(c, entry.Title).Grow(1).Ellipsis()
							ui.Text(c, entry.UserName).Muted().FontSize(12)
							if ui.Button(c, m.t("duplicates.open")).Outline().XSmall().Clicked() {
								m.dups = nil
								m.selectGroup(entry.GroupID)
								m.selectEntry(entry.ID)
							}
						})
					}
				}
			})
		})
	}, func() {
		if ui.Button(c, m.t("dialog.close")).Outline().Clicked() {
			m.dups = nil
		}
	})
}

// expiredDialog lists entries whose expiry date has passed.
func (m *Model) expiredDialog(c ui.C) {
	open := len(m.expired) > 0
	ui.ModalWith(c, &open, m.t("expired.title"), ui.ModalSpec{Width: 520, Height: 400}, func() {
		if len(m.expired) == 0 {
			ui.Text(c, m.t("expired.none")).Muted()
			return
		}
		ui.Scroll(c).FillWidth().Grow(1).Children(func() {
			ui.Column(c).FillWidth().Gap(6).Children(func() {
				for _, entry := range m.expired {
					entry := entry
					ui.Row(c).Gap(8).Items(ui.Center).Children(func() {
						ui.Text(c, entry.Title).Grow(1).Ellipsis()
						ui.Text(c, entry.GroupPath).Muted().FontSize(12)
						if ui.Button(c, m.t("expired.open")).Outline().XSmall().Clicked() {
							m.expired = nil
							m.selectGroup(entry.GroupID)
							m.selectEntry(entry.ID)
						}
					})
				}
			})
		})
	}, func() {
		if ui.Button(c, m.t("dialog.close")).Outline().Clicked() {
			m.expired = nil
		}
	})
}

// exportAttachment writes one attachment to a file the user chooses.
func (m *Model) exportAttachment(entryID, name string) {
	path, err := app.SaveFile(app.SaveFileOptions{Title: m.t("fileDialog.exportAttachment"), SuggestedName: name})
	if err != nil || path == "" {
		return
	}
	data, err := m.vault().Attachment(entryID, name)
	if err != nil {
		m.fail(err)
		return
	}
	if err := writeFile(path, data); err != nil {
		m.fail(err)
		return
	}
	m.info(m.t("toast.attachmentExported"))
}

// addAttachment attaches a file the user chooses to the edited entry.
func (m *Model) addAttachment() {
	paths, err := app.OpenFiles(app.OpenFileOptions{Title: m.t("fileDialog.selectAttachment")})
	if err != nil || len(paths) == 0 {
		return
	}
	path := paths[0]
	data, err := readFile(path)
	if err != nil {
		m.fail(err)
		return
	}
	if err := m.vault().AddAttachment(m.editor.entry.ID, filepath.Base(path), data); err != nil {
		m.fail(err)
		return
	}
	m.loadDetail()
	m.editor.entry = m.detail
	m.info(m.t("toast.saved"))
}

// splitTags turns "a; b" into the tag slice the database stores.
func splitTags(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ';' || r == ',' })
	tags := make([]string, 0, len(parts))
	for _, part := range parts {
		if text := strings.TrimSpace(part); text != "" {
			tags = append(tags, text)
		}
	}
	return tags
}
