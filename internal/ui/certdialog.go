//go:build windows

package ui

import (
	"log"

	"github.com/lxn/walk"

	"github.com/Georgy-Garnov/agendling/internal/caldav"
	"github.com/Georgy-Garnov/agendling/internal/i18n"
)

// confirmCertificate shows a server certificate the system does not trust and asks whether to
// trust it for the account; true when the user does.
func (a *App) confirmCertificate(owner walk.Form, info caldav.CertInfo) bool {
	dlg, err := walk.NewDialog(owner)
	if err != nil {
		log.Printf("certificate dialog: %v", err)
		return false
	}
	dlg.SetTitle(i18n.T("Server certificate"))
	dlg.SetFont(uiFont(10, false))
	dlg.SetBackground(solid(flWindow))
	root := walk.NewVBoxLayout()
	root.SetMargins(walk.Margins{})
	root.SetSpacing(0)
	dlg.SetLayout(root)
	dlg.SetMinMaxSize(walk.Size{Width: 560}, walk.Size{})
	body := box(dlg, true, walk.Margins{HNear: 20, VNear: 18, HFar: 20, VFar: 16}, 10)

	label(body, i18n.T("The server's certificate is not trusted"), uiFont(13, true), flText)
	caption(body, i18n.T("Your computer does not know who issued it. That is normal for a NAS or a home server with a "+
		"self-signed certificate, but it is also what an impostor's server would look like. Trust it only if this is "+
		"your own server and the details below match it."))

	card := formCard(body)
	issuer := info.Issuer
	if info.SelfSigned {
		issuer = i18n.Tf("%s (self-signed)", issuer)
	}
	for _, row := range [][2]string{
		{i18n.T("Issued to"), info.Subject},
		{i18n.T("Issued by"), issuer},
		{i18n.T("Valid until"), i18n.Date(info.NotAfter.Local(), "2 January 2006")},
	} {
		label(formRow(card, row[0]), row[1], uiFont(10, false), flText)
	}
	fp, _ := walk.NewTextLabel(formRow(card, "SHA-256"))
	fp.SetText(caldav.FingerprintLines(info.Fingerprint))
	fp.SetFont(cachedFont("Consolas", 10, 0))
	fp.SetTextColor(flText)
	fp.SetBackground(walk.NullBrush())

	sep, _ := walk.NewComposite(dlg)
	sep.SetBackground(solid(flLine))
	sep.SetMinMaxSize(walk.Size{Height: 1}, walk.Size{Height: 1})
	bottom := box(dlg, false, walk.Margins{HNear: 20, VNear: 14, HFar: 20, VFar: 14}, 8)
	bottom.SetBackground(solid(flSurface))
	walk.NewHSpacer(bottom)
	pushButton(bottom, i18n.T("Trust this certificate"), 180, func() { acceptLater(dlg) })
	cancel := pushButton(bottom, i18n.T("Cancel"), 96, func() { dlg.Cancel() })
	dlg.SetCancelButton(cancel)
	dlg.SetDefaultButton(cancel) // trusting must be a deliberate click
	a.applyIcon(dlg)
	if dialogHook != nil {
		dlg.Starting().Attach(func() { dialogHook("certificate", dlg) })
	}
	return dlg.Run() == walk.DlgCmdOK
}
