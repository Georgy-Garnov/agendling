//go:build windows

package ui

import (
	"log"

	"github.com/lxn/walk"

	"github.com/Georgy-Garnov/agendling/internal/caldav"
)

// confirmCertificate shows a server certificate the system does not trust and asks whether to
// trust it for the account; true when the user does.
func (a *App) confirmCertificate(owner walk.Form, info caldav.CertInfo) bool {
	dlg, err := walk.NewDialog(owner)
	if err != nil {
		log.Printf("certificate dialog: %v", err)
		return false
	}
	dlg.SetTitle("Server certificate")
	dlg.SetFont(uiFont(10, false))
	dlg.SetBackground(solid(flWindow))
	root := walk.NewVBoxLayout()
	root.SetMargins(walk.Margins{})
	root.SetSpacing(0)
	dlg.SetLayout(root)
	dlg.SetMinMaxSize(walk.Size{Width: 560}, walk.Size{})
	body := box(dlg, true, walk.Margins{HNear: 20, VNear: 18, HFar: 20, VFar: 16}, 10)

	label(body, "The server's certificate is not trusted", uiFont(13, true), flText)
	caption(body, "Your computer does not know who issued it. That is normal for a NAS or a home server with a "+
		"self-signed certificate, but it is also what an impostor's server would look like. Trust it only if this is "+
		"your own server and the details below match it.")

	card := formCard(body)
	issuer := info.Issuer
	if info.SelfSigned {
		issuer += " (self-signed)"
	}
	for _, row := range [][2]string{
		{"Issued to", info.Subject},
		{"Issued by", issuer},
		{"Valid until", info.NotAfter.Local().Format("2 January 2006")},
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
	pushButton(bottom, "Trust this certificate", 180, func() { acceptLater(dlg) })
	cancel := pushButton(bottom, "Cancel", 96, func() { dlg.Cancel() })
	dlg.SetCancelButton(cancel)
	dlg.SetDefaultButton(cancel) // trusting must be a deliberate click
	a.applyIcon(dlg)
	if dialogHook != nil {
		dlg.Starting().Attach(func() { dialogHook("certificate", dlg) })
	}
	return dlg.Run() == walk.DlgCmdOK
}
