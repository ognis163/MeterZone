// Package pdfdoc формирует PDF-документы (A4, кириллица, DejaVu Sans).
package pdfdoc

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"

	"indextro/internal/model"
	"indextro/internal/report"
)

const (
	pageW    = 210.0
	pageH    = 297.0
	marginL  = 18.0
	marginR  = 18.0
	topY     = 44.0
	bottomY  = 277.0
	contentW = pageW - marginL - marginR
)

var (
	colInk    = [3]int{24, 28, 32}
	colMuted  = [3]int{110, 118, 126}
	colLine   = [3]int{214, 219, 224}
	colFill   = [3]int{245, 247, 248}
	colAccent = [3]int{15, 92, 77}
)

type builder struct {
	pdf      *fpdf.Fpdf
	font     []byte
	title    string
	subtitle string
	address  string
}

func newBuilder(font []byte, title, subtitle, address string) *builder {
	if strings.TrimSpace(address) == "" {
		address = title
	}
	b := &builder{font: font, title: title, subtitle: subtitle, address: address}
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.AddUTF8FontFromBytes("dejavu", "", font)
	pdf.SetMargins(marginL, topY, marginR)
	pdf.SetAutoPageBreak(false, 0)
	pdf.AliasNbPages("{nb}")
	pdf.SetTitle(title, true)
	pdf.SetAuthor("", true)
	pdf.SetCreator("", true)
	pdf.SetSubject("Учёт показаний приборов учёта", true)

	pdf.SetHeaderFunc(func() {
		b.drawHeader(pdf.PageNo() > 1)
	})
	pdf.SetFooterFunc(func() {
		b.drawFooter()
	})
	b.pdf = pdf
	pdf.AddPage()
	return b
}

func (b *builder) drawHeader(cont bool) {
	p := b.pdf
	p.SetY(12)
	p.SetFont("dejavu", "", 7.5)
	setText(p, colMuted)
	p.CellFormat(110, 4, fitB(p, b.address, 108), "", 0, "L", false, 0, "")
	p.SetXY(marginL, 12)
	p.CellFormat(contentW, 4, "УЧЁТ ПОКАЗАНИЙ · ДОКУМЕНТ", "", 0, "R", false, 0, "")

	p.SetXY(marginL, 20)
	p.SetFont("dejavu", "", 17)
	setText(p, colInk)
	title := b.title
	if cont {
		title += " · продолжение"
	}
	p.CellFormat(contentW, 9, title, "", 0, "L", false, 0, "")

	p.SetXY(marginL, 30)
	p.SetFont("dejavu", "", 10)
	setText(p, colMuted)
	p.CellFormat(contentW, 5, b.subtitle, "", 0, "L", false, 0, "")

	p.SetDrawColor(colAccent[0], colAccent[1], colAccent[2])
	p.SetLineWidth(0.7)
	p.Line(marginL, 39, marginL+22, 39)
	p.SetDrawColor(colLine[0], colLine[1], colLine[2])
	p.SetLineWidth(0.25)
	p.Line(marginL+22, 39, pageW-marginR, 39)
}

func (b *builder) drawFooter() {
	p := b.pdf
	p.SetDrawColor(colLine[0], colLine[1], colLine[2])
	p.SetLineWidth(0.25)
	p.Line(marginL, pageH-16, pageW-marginR, pageH-16)
	p.SetY(pageH - 13)
	p.SetFont("dejavu", "", 7.5)
	setText(p, colMuted)
	p.CellFormat(contentW/2, 4, "Сформировано "+model.FormatDateTimeRu(time.Now()), "", 0, "L", false, 0, "")
	p.CellFormat(contentW/2, 4, fmt.Sprintf("стр. %d из {nb}", p.PageNo()), "", 0, "R", false, 0, "")
}

func setText(p *fpdf.Fpdf, c [3]int) { p.SetTextColor(c[0], c[1], c[2]) }

func (b *builder) newPage() {
	b.pdf.AddPage()
	b.pdf.SetY(topY)
}

// ensure переносит страницу, если не хватает места.
func (b *builder) ensure(h float64) {
	if b.pdf.GetY()+h > bottomY {
		b.newPage()
	}
}

// ---------- элементы

func (b *builder) objectBlock(o model.Object, scope string, period string, from, to model.Period) {
	p := b.pdf
	p.SetY(topY)

	type kv struct{ k, v string }
	rows := []kv{{"Собственник", dash(o.Owner)}}
	if o.Address != "" {
		rows = append(rows, kv{"Адрес объекта", o.Address})
	}
	acc := ""
	if scope == "all" || scope == "water" {
		if o.AccountWater != "" {
			acc = "Лицевой счёт (вода): " + o.AccountWater
		}
	}
	if scope == "all" || scope == "energy" {
		if o.AccountEnergy != "" {
			if acc != "" {
				acc += "    "
			}
			acc += "Лицевой счёт (электро): " + o.AccountEnergy
		}
	}
	if acc != "" {
		rows = append(rows, kv{"Реквизиты", acc})
	}

	labelW := 36.0
	for _, r := range rows {
		p.SetFont("dejavu", "", 7.5)
		setText(p, colMuted)
		p.CellFormat(labelW, 6, strings.ToUpper(r.k), "", 0, "L", false, 0, "")
		p.SetFont("dejavu", "", 10)
		setText(p, colInk)
		avail := contentW - labelW
		lines := p.SplitText(r.v, avail)
		if len(lines) == 0 {
			lines = []string{""}
		}
		for i, ln := range lines {
			if i > 0 {
				p.SetX(marginL + labelW)
			}
			p.CellFormat(avail, 6, ln, "", 1, "L", false, 0, "")
		}
	}
	p.Ln(3)
}

func (b *builder) sectionTitle(title, unit string) {
	b.ensure(16)
	p := b.pdf
	p.SetFont("dejavu", "", 11.5)
	setText(p, colInk)
	p.CellFormat(contentW-40, 8, title, "", 0, "L", false, 0, "")
	p.SetFont("dejavu", "", 8.5)
	setText(p, colMuted)
	p.CellFormat(40, 8, unit, "", 1, "R", false, 0, "")
	p.SetDrawColor(colLine[0], colLine[1], colLine[2])
	p.SetLineWidth(0.3)
	p.Line(marginL, p.GetY(), pageW-marginR, p.GetY())
	p.Ln(2)
}

type colDef struct {
	title string
	width float64
	align string
}

func (b *builder) tableHead(cols []colDef) {
	p := b.pdf
	b.ensure(10)
	y := p.GetY()
	p.SetFillColor(colFill[0], colFill[1], colFill[2])
	p.Rect(marginL, y, contentW, 8, "F")
	p.SetFont("dejavu", "", 7.8)
	setText(p, colMuted)
	x := marginL
	for _, c := range cols {
		p.SetXY(x, y+2)
		p.CellFormat(c.width, 4, c.title, "", 0, c.align, false, 0, "")
		x += c.width
	}
	p.SetDrawColor(colLine[0], colLine[1], colLine[2])
	p.Line(marginL, y+8, pageW-marginR, y+8)
	p.SetY(y + 8)
}

func (b *builder) tableRow(cells []string, cols []colDef, bold bool, fill bool) {
	p := b.pdf
	b.ensure(7.5)
	y := p.GetY()
	if fill {
		p.SetFillColor(250, 251, 252)
		p.Rect(marginL, y, contentW, 7.5, "F")
	}
	p.SetFont("dejavu", "", 9)
	setText(p, colInk)
	x := marginL
	for i, c := range cols {
		if i < len(cells) {
			p.SetXY(x, y+1.8)
			p.CellFormat(c.width, 4, cells[i], "", 0, c.align, false, 0, "")
		}
		x += c.width
	}
	p.SetDrawColor(232, 235, 238)
	p.SetLineWidth(0.2)
	p.Line(marginL, y+7.5, pageW-marginR, y+7.5)
	p.SetY(y + 7.5)
}

func (b *builder) emptyNote(text string) {
	p := b.pdf
	p.SetFont("dejavu", "", 10)
	setText(p, colMuted)
	p.MultiCell(contentW, 6, text, "", "L", false)
	p.Ln(2)
}

func (b *builder) signature(periodLabel string) {
	b.ensure(30)
	p := b.pdf
	p.Ln(8)
	p.SetFont("dejavu", "", 9)
	setText(p, colMuted)
	p.CellFormat(contentW/2, 8, "Показания сняты «____» ________________ 20____ г.", "", 0, "L", false, 0, "")
	p.CellFormat(contentW/2, 8, "Подпись ______________________", "", 1, "R", false, 0, "")
	p.SetY(p.GetY() + 2)
	p.SetFont("dejavu", "", 7.5)
	p.CellFormat(contentW, 4, "Документ сформирован автоматически.", "", 1, "L", false, 0, "")
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

// ---------- документы

// MonthPDF строит «Сведения о показаниях» за месяц.
func MonthPDF(rep report.Month, font []byte) ([]byte, error) {
	title := "Сведения о показаниях приборов учёта"
	switch rep.Scope {
	case "water":
		title = "Сведения о показаниях воды"
	case "energy":
		title = "Сведения о показаниях электроэнергии"
	}
	subtitle := "за " + rep.Period.Accusative() + " г."
	b := newBuilder(font, title, subtitle, rep.Object.Address)
	b.objectBlock(rep.Object, rep.Scope, rep.Period.String(), rep.Period, rep.Period)

	if len(rep.Sections) == 0 || !rep.HasReadings {
		b.pdf.SetY(topY + 4)
		b.emptyNote("Показания за выбранный месяц не вносились.")
		b.signature(rep.Period.Label())
		return b.output()
	}

	cols := []colDef{
		{"Прибор / место", 56, "L"},
		{"Заводской №", 38, "L"},
		{"Предыдущее", 26, "R"},
		{"Текущее", 26, "R"},
		{"Расход", 28, "R"},
	}
	cols[4].width = contentW - cols[0].width - cols[1].width - cols[2].width - cols[3].width

	hasSubRows := false
	for _, s := range rep.Sections {
		b.sectionTitle(s.Resource.Section(), "расход, "+s.Unit)
		head := []colDef{
			{cols[0].title, cols[0].width, cols[0].align},
			{cols[1].title, cols[1].width, cols[1].align},
			{cols[2].title, cols[2].width, cols[2].align},
			{cols[3].title, cols[3].width, cols[3].align},
			{"Расход, " + s.Unit, cols[4].width, cols[4].align},
		}
		b.tableHead(head)
		for i, r := range s.Rows {
			prev, curr, diff := "—", "—", "—"
			if r.HasPrev {
				prev = model.FormatValue(r.Prev)
			}
			if r.HasCurr {
				curr = model.FormatValue(r.Curr)
			}
			if r.HasDiff {
				diff = model.FormatValue(r.Diff)
			}
			label := r.Meter.Title()
			if r.IsSub {
				if !strings.Contains(strings.ToLower(label), "суб") {
					label += " · суб"
				}
				hasSubRows = true
			}
			serial := dash(r.Meter.Serial)
			if len([]rune(serial)) > 22 {
				serial = string([]rune(serial)[:21]) + "…"
			}
			b.tableRow([]string{fitB(b.pdf, label, cols[0].width-2), serial, prev, curr, diff}, head, false, i%2 == 1)
		}
		b.pdf.Ln(5)
	}

	if hasSubRows {
		b.pdf.SetFont("dejavu", "", 8)
		setText(b.pdf, colMuted)
		b.pdf.MultiCell(contentW, 4.5, "Строка «суб» — показания суб-счётчика; расход основного прибора указан за вычетом суб-счётчиков.", "", "L", false)
		b.pdf.Ln(2)
	}
	if rep.Missing > 0 {
		b.pdf.SetFont("dejavu", "", 8)
		setText(b.pdf, colMuted)
		b.pdf.MultiCell(contentW, 4.5, "Прочерк (—) означает, что показание за выбранный месяц не внесено.", "", "L", false)
		b.pdf.Ln(2)
	}
	b.signature(rep.Period.Label())
	return b.output()
}

// RangePDF строит «Ведомость показаний» за период.
func RangePDF(rep report.Range, font []byte) ([]byte, error) {
	title := "Ведомость показаний"
	switch rep.Scope {
	case "water":
		title = "Ведомость показаний воды"
	case "energy":
		title = "Ведомость показаний электроэнергии"
	}
	var sub string
	toMonth := strings.ToLower(rep.To.MonthName())
	if rep.From.Year == rep.To.Year {
		sub = fmt.Sprintf("за период с %s по %s %d г.", rep.From.Genitive(), toMonth, rep.To.Year)
	} else {
		sub = fmt.Sprintf("за период с %s по %s", rep.From.Genitive(), toMonth+" "+strconv.Itoa(rep.To.Year))
	}
	b := newBuilder(font, title, sub, rep.Object.Address)
	b.objectBlock(rep.Object, rep.Scope, "", rep.From, rep.To)

	if len(rep.Resources) == 0 || len(rep.Rows) == 0 {
		b.pdf.SetY(topY + 4)
		b.emptyNote("За выбранный период данных нет.")
		return b.output()
	}

	monthW := 40.0
	rest := contentW - monthW
	colW := rest / float64(len(rep.Resources))
	cols := []colDef{{"Месяц", monthW, "L"}}
	for _, r := range rep.Resources {
		cols = append(cols, colDef{r.Short() + ", " + r.Unit(), colW, "R"})
	}

	// Шапка таблицы повторяется на каждой странице.
	drawHead := func() {
		b.tableHead(cols)
	}
	drawHead()

	for _, row := range rep.Rows {
		if b.pdf.GetY()+7.5 > bottomY {
			b.newPage()
			drawHead()
		}
		cells := []string{row.Period.Label()}
		for _, c := range row.Cells {
			if c.Has {
				cells = append(cells, model.FormatValue(c.Consumption))
			} else {
				cells = append(cells, "—")
			}
		}
		b.tableRow(cells, cols, false, false)
	}

	// Итого по ресурсам.
	b.ensure(12)
	y := b.pdf.GetY()
	b.pdf.SetDrawColor(colInk[0], colInk[1], colInk[2])
	b.pdf.SetLineWidth(0.4)
	b.pdf.Line(marginL, y, pageW-marginR, y)
	b.pdf.SetY(y)
	b.pdf.SetFont("dejavu", "", 9)
	setText(b.pdf, colInk)
	b.pdf.CellFormat(monthW, 8, "Итого", "", 0, "L", false, 0, "")
	for _, r := range rep.Resources {
		b.pdf.CellFormat(colW, 8, model.FormatValue(rep.ResourceTotals[r]), "", 0, "R", false, 0, "")
	}
	b.pdf.Ln(8)

	// Приборы учёта.
	b.ensure(58)
	b.pdf.Ln(6)
	b.pdf.SetFont("dejavu", "", 11.5)
	setText(b.pdf, colInk)
	b.pdf.CellFormat(contentW, 8, "Приборы учёта", "", 1, "L", false, 0, "")
	b.pdf.SetDrawColor(colLine[0], colLine[1], colLine[2])
	b.pdf.SetLineWidth(0.3)
	b.pdf.Line(marginL, b.pdf.GetY(), pageW-marginR, b.pdf.GetY())
	b.pdf.Ln(1)

	mcols := []colDef{
		{"Прибор", 72, "L"},
		{"Ресурс", 42, "L"},
		{"Показание", 34, "R"},
		{"Расход", 26, "R"},
	}
	mcols[3].width = contentW - mcols[0].width - mcols[1].width - mcols[2].width
	b.tableHead(mcols)
	for i, mt := range rep.MeterTotals {
		label := mt.Meter.Title()
		if mt.IsSub && !strings.Contains(strings.ToLower(label), "суб") {
			label += " · суб"
		}
		label = fitB(b.pdf, label, mcols[0].width-2)
		last := "—"
		if mt.HasLast {
			last = model.FormatValue(mt.LastValue)
		}
		total := "—"
		if mt.HasTotal {
			total = model.FormatValue(mt.Total)
		}
		b.tableRow([]string{
			fitB(b.pdf, label, mcols[0].width-2),
			mt.Meter.Resource.Label(),
			last,
			total,
		}, mcols, false, i%2 == 1)
	}

	b.signature("")
	return b.output()
}

func fitB(p *fpdf.Fpdf, s string, max float64) string {
	if p.GetStringWidth(s) <= max {
		return s
	}
	r := []rune(s)
	for len(r) > 1 {
		r = r[:len(r)-1]
		cand := string(r) + "…"
		if p.GetStringWidth(cand) <= max {
			return cand
		}
	}
	return "…"
}

func (b *builder) output() ([]byte, error) {
	var buf bytes.Buffer
	if err := b.pdf.Output(&buf); err != nil {
		return nil, err
	}
	if err := b.pdf.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
