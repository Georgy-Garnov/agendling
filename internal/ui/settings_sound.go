//go:build windows

package ui

import (
	"fmt"
	"strings"

	"github.com/lxn/walk"

	"github.com/Georgy-Garnov/agendling/internal/core"
	"github.com/Georgy-Garnov/agendling/internal/model"
)

// soundOption is a "custom sound" switch and the file row it reveals.
type soundOption struct {
	on   *toggleSwitch
	row  *walk.Composite
	path *walk.LineEdit
}

func (o *soundOption) sound() model.Sound {
	return core.CleanSound(model.Sound{Custom: o.on.Checked(), Path: o.path.Text()})
}

// calendarSounds are the two sound options of one card under "By calendar".
type calendarSounds struct {
	key                string
	reminder, newEvent *soundOption
}

func (s *settingsUI) buildSound() {
	a := s.a
	p := s.addPage("sound")
	row := s.card(p, "Alert sound", "Used for every alert unless a kind of alert or a calendar below has a sound "+
		"of its own. An MP3 or WAV file; leave empty for the built-in chime.", true)
	s.soundPath, _ = walk.NewLineEdit(row)
	s.soundPath.SetText(s.saved.CustomSoundPath)
	s.soundPath.SetCueBanner("Built-in chime")
	s.soundPath.SetMinMaxSize(walk.Size{Height: 30}, walk.Size{Height: 30})
	row.Layout().(*walk.BoxLayout).SetStretchFactor(s.soundPath, 1)
	s.soundPath.TextChanged().Attach(s.updateApply)
	pushButton(row, "Browse…", 90, func() { s.browseSound(s.soundPath) })
	pushButton(row, "Clear", 80, func() { s.soundPath.SetText("") })

	right := s.card(p, "Try it", "Plays the chosen sound once.", false)
	pushButton(right, "▶  Test", 90, func() { s.testSound(s.soundPath.Text()) })
	pushButton(right, "■  Stop", 90, a.audio.Stop)

	right = s.card(p, "Repeat until stopped", "Keep playing the sound until you press Stop Sound or the maximum "+
		"length below is reached.", false)
	s.loop, _ = newToggle(right, s.saved.LoopSound, flSurface, s.updateApply)

	right = s.card(p, "Maximum length", "How long a sound may play.", false)
	limit := s.saved.SoundLimitSeconds
	if limit <= 0 {
		limit = model.DefaultSettings().SoundLimitSeconds
	}
	number := func(value, max int) *walk.NumberEdit {
		n, _ := walk.NewNumberEdit(right)
		n.SetRange(0, float64(max))
		n.SetDecimals(0)
		n.SetValue(float64(value))
		n.SetSpinButtonsVisible(true)
		n.SetMinMaxSize(walk.Size{Width: 64, Height: 30}, walk.Size{Width: 64, Height: 30})
		n.ValueChanged().Attach(s.updateApply)
		return n
	}
	s.limitMin = number(limit/60, 999)
	label(right, "min", uiFont(10, false), flText2)
	s.limitSec = number(limit%60, 59)
	label(right, "sec", uiFont(10, false), flText2)
	gap, _ := walk.NewComposite(right)
	gap.SetMinMaxSize(walk.Size{Width: 12}, walk.Size{Width: 12})
	gap.SetBackground(walk.NullBrush())
	label(right, "No limit", uiFont(10, false), flText)
	s.noLimit, _ = newToggle(right, s.saved.SoundLimitSeconds <= 0, flSurface, func() {
		s.limitMin.SetEnabled(!s.noLimit.Checked())
		s.limitSec.SetEnabled(!s.noLimit.Checked())
		s.updateApply()
	})
	s.limitMin.SetEnabled(!s.noLimit.Checked())
	s.limitSec.SetEnabled(!s.noLimit.Checked())

	s.section(p, "By kind of alert", "Switch on a sound of its own for reminders or for new meetings; otherwise "+
		"they use the alert sound above.")
	c, top := s.soundCard(p, "Meeting reminders", "Before an event starts.", nil)
	s.typeReminder = s.soundOption(top, c, s.saved.TypeSounds.Reminder)
	c, top = s.soundCard(p, "New meetings", "When a sync brings in a new upcoming event, e.g. an invitation.", nil)
	s.typeNewEvent = s.soundOption(top, c, s.saved.TypeSounds.NewEvent)

	s.section(p, "By calendar", "A calendar's own sound comes first, then its account's, then the sounds above.")
	s.calSounds = nil
	for _, t := range a.svc.SoundTargets() {
		parent := p
		if t.Child {
			parent = box(p, true, walk.Margins{HNear: 28}, 0)
		}
		color := ParseColor(t.Source.Color)
		c, _ := s.soundCard(parent, t.Name, t.Detail, &color)
		snd := s.saved.CalendarSounds[t.Key]
		cs := &calendarSounds{key: t.Key}
		cs.reminder = s.soundOption(s.optionLine(c, "Reminder sound"), c, snd.Reminder)
		cs.newEvent = s.soundOption(s.optionLine(c, "New meeting sound"), c, snd.NewEvent)
		s.calSounds = append(s.calSounds, cs)
	}
}

// section is a heading between groups of cards.
func (s *settingsUI) section(page *walk.Composite, title, desc string) {
	b := box(page, true, walk.Margins{VNear: 14, VFar: 2}, 2)
	label(b, title, uiFont(12, true), flText)
	caption(b, desc)
}

// soundCard is a card with a name and description (and a colour dot for calendars). It returns
// the card, for the rows below, and its top line, for a control at the right.
func (s *settingsUI) soundCard(page *walk.Composite, name, desc string, dot *walk.Color) (card, top *walk.Composite) {
	c := box(page, true, walk.Margins{HNear: 16, VNear: 12, HFar: 16, VFar: 12}, 8)
	makeCard(c, flWindow)
	top = box(c, false, walk.Margins{}, 12)
	if dot != nil {
		newColorDot(top, *dot, flSurface)
	}
	text := box(top, true, walk.Margins{}, 2)
	walk.NewHSpacer(text) // the text column takes the free width; the switch sits at the right
	label(text, name, uiFont(10, dot != nil), flText)
	if desc != "" {
		caption(text, desc)
	}
	return c, top
}

// optionLine is a line in a calendar card with the option's name; the switch goes at its right.
func (s *settingsUI) optionLine(card *walk.Composite, name string) *walk.Composite {
	line := box(card, false, walk.Margins{HNear: 28}, 12)
	label(line, name, uiFont(10, false), flText)
	walk.NewHSpacer(line)
	return line
}

// soundOption adds the switch to line and, below it in card, the file row shown while it is on.
func (s *settingsUI) soundOption(line, card *walk.Composite, snd model.Sound) *soundOption {
	o := &soundOption{}
	o.row = box(card, false, walk.Margins{HNear: 28}, 8)
	o.path, _ = walk.NewLineEdit(o.row)
	o.path.SetText(snd.Path)
	o.path.SetCueBanner("Built-in chime")
	o.path.SetMinMaxSize(walk.Size{Height: 30}, walk.Size{Height: 30})
	o.row.Layout().(*walk.BoxLayout).SetStretchFactor(o.path, 1)
	o.path.TextChanged().Attach(s.updateApply)
	pushButton(o.row, "Browse…", 90, func() { s.browseSound(o.path) })
	pushButton(o.row, "▶", 40, func() { s.testSound(o.path.Text()) })
	pushButton(o.row, "■", 40, s.a.audio.Stop)
	o.row.SetVisible(snd.Custom)
	o.on, _ = newToggle(line, snd.Custom, flSurface, func() {
		o.row.SetVisible(o.on.Checked())
		s.updateApply()
	})
	return o
}

func (s *settingsUI) browseSound(path *walk.LineEdit) {
	fd := &walk.FileDialog{Title: "Choose alert sound", Filter: "Audio files (*.mp3;*.wav)|*.mp3;*.wav|All files (*.*)|*.*"}
	if ok, _ := fd.ShowOpen(s.dlg); ok {
		path.SetText(fd.FilePath)
	}
}

func (s *settingsUI) testSound(path string) {
	path = strings.TrimSpace(path)
	go func() {
		if err := s.a.audio.Play(path); err != nil {
			s.dlg.Synchronize(func() { walk.MsgBox(s.dlg, "Sound", err.Error(), walk.MsgBoxIconWarning) })
		}
	}()
}

// collectSound puts the Sound page into st.
func (s *settingsUI) collectSound(st *model.AppSettings) error {
	st.CustomSoundPath = strings.TrimSpace(s.soundPath.Text())
	st.LoopSound = s.loop.Checked()
	st.SoundLimitSeconds = 0
	if !s.noLimit.Checked() {
		st.SoundLimitSeconds = int(s.limitMin.Value())*60 + int(s.limitSec.Value())
		if st.SoundLimitSeconds <= 0 {
			return fmt.Errorf("the maximum sound length must be at least one second, or switch on No limit")
		}
	}
	st.TypeSounds = model.AlertSounds{Reminder: s.typeReminder.sound(), NewEvent: s.typeNewEvent.sound()}
	sounds := map[string]model.AlertSounds{}
	for k, v := range st.CalendarSounds { // keeps sounds of calendars not listed, e.g. unsubscribed ones
		sounds[k] = v
	}
	for _, cs := range s.calSounds {
		sounds[cs.key] = model.AlertSounds{Reminder: cs.reminder.sound(), NewEvent: cs.newEvent.sound()}
	}
	st.CalendarSounds = core.CleanCalendarSounds(sounds)
	return nil
}
