//go:build linux

package gtkui

import (
	"github.com/gotk3/gotk3/glib"
	"github.com/gotk3/gotk3/gtk"

	"github.com/Georgy-Garnov/agendling/internal/caldav"
	"github.com/Georgy-Garnov/agendling/internal/i18n"
)

// confirmCertificate shows a server certificate the system does not trust and asks whether to
// trust it for the account; true when the user does.
func (a *App) confirmCertificate(parent gtk.IWindow, info caldav.CertInfo) bool {
	dlg, err := gtk.DialogNew()
	if err != nil {
		return false
	}
	defer dlg.Destroy()
	dlg.SetTransientFor(parent)
	dlg.SetModal(true)
	dlg.SetTitle(i18n.T("Server certificate"))
	dlg.SetResizable(false) // as tall as its content
	addClass(dlg, "settings-dialog")
	dlg.AddButton(i18n.T("Cancel"), gtk.RESPONSE_CANCEL)
	dlg.AddButton(i18n.T("Trust this certificate"), gtk.RESPONSE_ACCEPT)
	dlg.SetDefaultResponse(gtk.RESPONSE_CANCEL) // trusting must be a deliberate click

	body, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 10)
	body.SetMarginStart(18)
	body.SetMarginEnd(18)
	body.SetMarginTop(16)
	body.SetMarginBottom(12)
	title := markupLabel(`<span size="large"><b>` + glib.MarkupEscapeText(i18n.T("The server's certificate is not trusted")) + `</b></span>`)
	title.SetXAlign(0)
	body.PackStart(title, false, false, 0)
	intro, _ := gtk.LabelNew(i18n.T("Your computer does not know who issued it. That is normal for a NAS or a home server with a " +
		"self-signed certificate, but it is also what an impostor's server would look like. Trust it only if this is " +
		"your own server and the details below match it."))
	intro.SetXAlign(0)
	intro.SetLineWrap(true)
	intro.SetMaxWidthChars(64)
	addClass(intro, "settings-caption")
	body.PackStart(intro, false, false, 0)

	grid, _ := gtk.GridNew()
	grid.SetRowSpacing(8)
	grid.SetColumnSpacing(14)
	addClass(grid, "settings-card")
	issuer := info.Issuer
	if info.SelfSigned {
		issuer = i18n.Tf("%s (self-signed)", issuer)
	}
	for i, row := range [][2]string{
		{i18n.T("Issued to"), info.Subject},
		{i18n.T("Issued by"), issuer},
		{i18n.T("Valid until"), i18n.Date(info.NotAfter.Local(), "2 January 2006")},
		{"SHA-256", caldav.FingerprintLines(info.Fingerprint)},
	} {
		k, _ := gtk.LabelNew(row[0])
		k.SetXAlign(0)
		k.SetYAlign(0)
		addClass(k, "settings-field")
		text := glib.MarkupEscapeText(row[1])
		if row[0] == "SHA-256" {
			text = "<tt>" + text + "</tt>"
		}
		v := markupLabel(text)
		v.SetXAlign(0)
		v.SetLineWrap(true)
		v.SetMaxWidthChars(48)
		v.SetSelectable(true)
		grid.Attach(k, 0, i, 1, 1)
		grid.Attach(v, 1, i, 1, 1)
	}
	body.PackStart(grid, false, false, 0)
	content, _ := dlg.GetContentArea()
	content.PackStart(body, false, false, 0)
	dlg.ShowAll()
	return dlg.Run() == gtk.RESPONSE_ACCEPT
}
