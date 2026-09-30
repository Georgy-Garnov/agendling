//go:build windows

package ui

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"

	"github.com/Georgy-Garnov/agendling/internal/appinfo"
	"github.com/Georgy-Garnov/agendling/internal/caldav"
	"github.com/Georgy-Garnov/agendling/internal/core"
	"github.com/Georgy-Garnov/agendling/internal/model"
)

// ---------- sources table model ----------

type sourceModel struct {
	walk.TableModelBase
	items []*model.CalendarSource
}

func (m *sourceModel) RowCount() int { return len(m.items) }

func (m *sourceModel) Value(row, col int) interface{} {
	s := m.items[row]
	switch col {
	case 0:
		return s.Name
	case 1:
		if s.IsLocal() {
			return "Local"
		}
		return "CalDAV"
	case 2:
		return core.SourceStatus(s)
	}
	return ""
}

// ---------- settings dialog ----------

func (a *App) showSettings() {
	st := a.store.Settings()
	var (
		dlg                      *walk.Dialog
		table                    *walk.TableView
		rems, soundPath          *walk.LineEdit
		snooze                   *walk.NumberEdit
		loop, startup, minimized *walk.CheckBox
		newAlerts                *walk.CheckBox
		acceptBtn, cancelBtn     *walk.PushButton
	)
	m := &sourceModel{}
	reload := func() {
		m.items, _ = a.store.Sources()
		m.PublishRowsReset()
	}
	reload()
	sourcesChanged := false

	selected := func() *model.CalendarSource {
		i := table.CurrentIndex()
		if i < 0 || i >= len(m.items) {
			return nil
		}
		return m.items[i]
	}
	editSource := func(src *model.CalendarSource) {
		if a.editSource(dlg, src) {
			sourcesChanged = true
			reload()
		}
	}

	err := Dialog{
		AssignTo:      &dlg,
		Title:         "Settings",
		MinSize:       Size{Width: 640, Height: 480},
		DefaultButton: &acceptBtn,
		CancelButton:  &cancelBtn,
		Layout:        VBox{},
		Children: []Widget{
			TabWidget{
				Pages: []TabPage{
					{
						Title:  "Calendars",
						Layout: VBox{},
						Children: []Widget{
							TableView{
								AssignTo: &table,
								Columns: []TableViewColumn{
									{Title: "Name", Width: 180},
									{Title: "Type", Width: 70},
									{Title: "Status", Width: 300},
								},
								Model: m,
								StyleCell: func(style *walk.CellStyle) {
									if style.Col() == 0 && style.Row() < len(m.items) {
										c := ParseColor(m.items[style.Row()].Color)
										style.BackgroundColor = Blend(c, 0.55)
									}
								},
								OnItemActivated: func() {
									if s := selected(); s != nil {
										editSource(s)
									}
								},
							},
							Composite{
								Layout: HBox{MarginsZero: true},
								Children: []Widget{
									PushButton{Text: "Add CalDAV account…", OnClicked: func() {
										editSource(&model.CalendarSource{Type: model.SourceCalDAV, Enabled: true, SyncInterval: 15, Color: "#0b8043"})
									}},
									PushButton{Text: "Add local calendar…", OnClicked: func() {
										editSource(&model.CalendarSource{Type: model.SourceLocal, Enabled: true, Color: "#8e24aa"})
									}},
									PushButton{Text: "Edit…", OnClicked: func() {
										if s := selected(); s != nil {
											editSource(s)
										}
									}},
									PushButton{Text: "Remove", OnClicked: func() {
										s := selected()
										if s == nil {
											return
										}
										msg := fmt.Sprintf("Remove calendar \"%s\"?", s.Name)
										if s.IsLocal() {
											msg += "\n\nAll its events will be permanently deleted."
										} else {
											msg += "\n\nLocal copies of its events are removed; the server is not changed."
										}
										if walk.MsgBox(dlg, "Remove calendar", msg, walk.MsgBoxOKCancel|walk.MsgBoxIconWarning) != walk.DlgCmdOK {
											return
										}
										if err := a.store.DeleteSource(s.ID); err != nil {
											walk.MsgBox(dlg, "Error", err.Error(), walk.MsgBoxIconError)
										}
										sourcesChanged = true
										reload()
									}},
									HSpacer{},
									PushButton{Text: "Sync now", OnClicked: func() {
										a.engine.SyncNow()
										time.AfterFunc(3*time.Second, func() { dlg.Synchronize(reload) })
									}},
								},
							},
						},
					},
					{
						Title:  "Notifications",
						Layout: Grid{Columns: 2},
						Children: []Widget{
							Label{Text: "Default reminders:"},
							LineEdit{AssignTo: &rems, Text: core.FormatReminders(st.DefaultReminderMinutes), CueBanner: "e.g. 15, 1h, 1d"},
							Label{},
							Label{Text: "Used for events without their own reminders. Units: m, h, d, w.", TextColor: walk.RGB(110, 110, 110)},

							Label{Text: "Snooze interval (min):"},
							NumberEdit{AssignTo: &snooze, Value: float64(st.SnoozeIntervalMinutes), MinValue: 1, MaxValue: 1440, Decimals: 0, MaxSize: Size{Width: 80}},

							Label{Text: "Alert sound:"},
							Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
								LineEdit{AssignTo: &soundPath, Text: st.CustomSoundPath, CueBanner: "built-in chime"},
								PushButton{Text: "Browse…", OnClicked: func() {
									fd := &walk.FileDialog{Title: "Choose alert sound", Filter: "Audio files (*.mp3;*.wav)|*.mp3;*.wav|All files (*.*)|*.*"}
									if ok, _ := fd.ShowOpen(dlg); ok {
										soundPath.SetText(fd.FilePath)
									}
								}},
								PushButton{Text: "Clear", OnClicked: func() { soundPath.SetText("") }},
							}},
							Label{},
							Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
								PushButton{Text: "▶ Test", OnClicked: func() {
									path := strings.TrimSpace(soundPath.Text())
									go func() {
										if err := a.audio.Play(path, false); err != nil {
											dlg.Synchronize(func() { walk.MsgBox(dlg, "Sound", err.Error(), walk.MsgBoxIconWarning) })
										}
									}()
								}},
								PushButton{Text: "■ Stop", OnClicked: a.audio.Stop},
								HSpacer{},
							}},
							Label{},
							CheckBox{AssignTo: &loop, Text: "Repeat the sound until stopped (max 3 minutes)", Checked: st.LoopSound},
							Label{},
							CheckBox{AssignTo: &newAlerts, Text: "Notify (sound + popup) when a new event appears after sync", Checked: !st.NewEventAlertsOff},
							VSpacer{ColumnSpan: 2},
						},
					},
					{
						Title:  "Behavior",
						Layout: VBox{},
						Children: []Widget{
							CheckBox{AssignTo: &startup, Text: "Run at Windows startup", Checked: st.RunAtStartup},
							CheckBox{AssignTo: &minimized, Text: "Start minimized to the system tray", Checked: st.StartMinimized},
							Label{Text: "Closing the main window keeps the app running in the tray. Use Exit in the tray menu to quit.", TextColor: walk.RGB(110, 110, 110)},
							VSpacer{},
						},
					},
				},
			},
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					HSpacer{},
					PushButton{AssignTo: &acceptBtn, Text: "OK", OnClicked: func() {
						mins, err := core.ParseReminders(rems.Text())
						if err != nil {
							walk.MsgBox(dlg, "Invalid reminders", err.Error(), walk.MsgBoxIconWarning)
							return
						}
						st := a.store.Settings()
						st.DefaultReminderMinutes = mins
						st.SnoozeIntervalMinutes = int(snooze.Value())
						st.CustomSoundPath = strings.TrimSpace(soundPath.Text())
						st.LoopSound = loop.Checked()
						st.NewEventAlertsOff = !newAlerts.Checked()
						st.RunAtStartup = startup.Checked()
						st.StartMinimized = minimized.Checked()
						if err := SetRunAtStartup(st.RunAtStartup); err != nil {
							walk.MsgBox(dlg, "Startup", "Could not update startup registration: "+err.Error(), walk.MsgBoxIconWarning)
						}
						if err := a.store.SaveSettings(st); err != nil {
							walk.MsgBox(dlg, "Error", err.Error(), walk.MsgBoxIconError)
							return
						}
						dlg.Accept()
					}},
					PushButton{AssignTo: &cancelBtn, Text: "Cancel", OnClicked: func() { dlg.Cancel() }},
				},
			},
		},
	}.Create(a.mw)
	if err != nil {
		log.Printf("settings: %v", err)
		return
	}
	dlg.Run()
	a.audio.Stop()
	if sourcesChanged {
		a.engine.Restart()
		a.view.Reload()
	}
}

// ---------- calendar source dialog ----------

func (a *App) editSource(owner walk.Form, src *model.CalendarSource) bool {
	isCalDAV := src.Type == model.SourceCalDAV
	var (
		dlg                                *walk.Dialog
		name, urlEdit, user, pass, colorEd *walk.LineEdit
		interval                           *walk.ComboBox
		enabled                            *walk.CheckBox
		swatch                             *walk.CustomWidget
		testBtn, acceptBtn, cancelBtn      *walk.PushButton
		testResult                         *walk.Label
	)
	intervalLabels := make([]string, len(core.SyncIntervals))
	intervalIdx := 2
	for i, it := range core.SyncIntervals {
		intervalLabels[i] = it.Label
		if it.Mins == src.SyncInterval {
			intervalIdx = i
		}
	}

	updateSwatch := func() {
		if swatch != nil {
			swatch.Invalidate()
		}
	}
	collect := func() *model.CalendarSource {
		s := *src
		s.Name = strings.TrimSpace(name.Text())
		s.Color = FormatColor(ParseColor(colorEd.Text()))
		s.Enabled = enabled.Checked()
		if isCalDAV {
			s.URL = strings.TrimSpace(urlEdit.Text())
			s.Username = strings.TrimSpace(user.Text())
			s.Password = pass.Text()
			if i := interval.CurrentIndex(); i >= 0 {
				s.SyncInterval = core.SyncIntervals[i].Mins
			}
			if s.URL != src.URL {
				s.CalendarPath = "" // re-discover for a different server
			}
		}
		return &s
	}

	children := []Widget{
		Label{Text: "Name:"},
		LineEdit{AssignTo: &name, Text: src.Name},
		Label{Text: "Color:"},
		Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
			CustomWidget{AssignTo: &swatch, MinSize: Size{Width: 22, Height: 22}, MaxSize: Size{Width: 22, Height: 22},
				PaintPixels: func(c *walk.Canvas, _ walk.Rectangle) error {
					b, err := walk.NewSolidColorBrush(ParseColor(colorEd.Text()))
					if err != nil {
						return err
					}
					defer b.Dispose()
					return c.FillRectanglePixels(b, swatch.ClientBoundsPixels())
				}},
			LineEdit{AssignTo: &colorEd, Text: src.Color, MaxSize: Size{Width: 90}, OnTextChanged: func() { updateSwatch() }},
			PushButton{Text: "Pick…", OnClicked: func() {
				if c, ok := PickColor(dlg, ParseColor(colorEd.Text())); ok {
					colorEd.SetText(FormatColor(c))
				}
			}},
			HSpacer{},
		}},
	}
	if isCalDAV {
		children = append(children,
			Label{Text: "Server URL:"},
			LineEdit{AssignTo: &urlEdit, Text: src.URL, CueBanner: "https://caldav.example.com"},
			Label{},
			Label{TextColor: walk.RGB(110, 110, 110), Text: "Examples: Nextcloud — https://host/remote.php/dav ·  Fastmail — https://caldav.fastmail.com\r\n" +
				"A URL pointing at a single calendar collection syncs only that calendar."},
			Label{Text: "Username:"},
			LineEdit{AssignTo: &user, Text: src.Username},
			Label{Text: "App password:"},
			LineEdit{AssignTo: &pass, Text: src.Password, PasswordMode: true},
			Label{Text: "Sync:"},
			ComboBox{AssignTo: &interval, Model: intervalLabels, CurrentIndex: intervalIdx},
			Label{},
			Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
				PushButton{AssignTo: &testBtn, Text: "Test connection", OnClicked: func() {
					s := collect()
					testBtn.SetEnabled(false)
					testResult.SetText("Connecting…")
					go func() {
						ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
						defer cancel()
						n, err := caldav.TestConnection(ctx, s)
						dlg.Synchronize(func() {
							testBtn.SetEnabled(true)
							if err != nil {
								testResult.SetText("✖ " + err.Error())
							} else {
								testResult.SetText(fmt.Sprintf("✔ Connected, %d calendar(s) found", n))
							}
						})
					}()
				}},
				Label{AssignTo: &testResult, Text: ""},
				HSpacer{},
			}},
		)
	}
	children = append(children,
		Label{},
		CheckBox{AssignTo: &enabled, Text: "Enabled (shown and synchronized)", Checked: src.Enabled},
		VSpacer{ColumnSpan: 2},
		Composite{ColumnSpan: 2, Layout: HBox{MarginsZero: true}, Children: []Widget{
			HSpacer{},
			PushButton{AssignTo: &acceptBtn, Text: "OK", OnClicked: func() {
				s := collect()
				if s.Name == "" {
					walk.MsgBox(dlg, appinfo.Name, "Please enter a name.", walk.MsgBoxIconWarning)
					return
				}
				if isCalDAV && (s.URL == "" || s.Username == "") {
					walk.MsgBox(dlg, appinfo.Name, "Server URL and username are required.", walk.MsgBoxIconWarning)
					return
				}
				if err := a.store.SaveSource(s); err != nil {
					walk.MsgBox(dlg, "Error", err.Error(), walk.MsgBoxIconError)
					return
				}
				*src = *s
				dlg.Accept()
			}},
			PushButton{AssignTo: &cancelBtn, Text: "Cancel", OnClicked: func() { dlg.Cancel() }},
		}},
	)

	title := "Local calendar"
	if isCalDAV {
		title = "CalDAV account"
	}
	err := Dialog{
		AssignTo:      &dlg,
		Title:         title,
		MinSize:       Size{Width: 560, Height: 300},
		DefaultButton: &acceptBtn,
		CancelButton:  &cancelBtn,
		Layout:        Grid{Columns: 2},
		Children:      children,
	}.Create(owner)
	if err != nil {
		log.Printf("source dialog: %v", err)
		return false
	}
	return dlg.Run() == walk.DlgCmdOK
}
