package docgen

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/gpdf-dev/gpdf/document"
	glayout "indextro/internal/third_party/layout"
	grender "indextro/internal/third_party/render"
	"github.com/gpdf-dev/gpdf/pdf"
	"github.com/gpdf-dev/gpdf/pdf/font"

	"indextro/internal/model"
)

// PDF собирает PDF-файл документа через gpdf.
//
// Таблицы идут потоком: gpdf сам переносит их на следующие страницы и
// повторяет шапку таблицы. Шапка документа, подпись, шапка «продолжение»
// и подвал рисуются прямо на странице начальником отрисовки gpdf, поэтому
// стоят на тех же координатах, что и в doc.css.
func PDF(doc Document, fontData []byte) ([]byte, error) {
	ttf, err := font.ParseTrueType(fontData)
	if err != nil {
		return nil, fmt.Errorf("документ: шрифт: %w", err)
	}
	g := &board{ttf: ttf, fontData: fontData, doc: doc}
	return g.generate()
}

// ---------- миллиметры и стили ----------

func mm(v float64) float64 { return v * 2.83465 }

func pt(v float64) document.Value { return document.Pt(v) }

type board struct {
	ttf      *font.TrueTypeFont
	fontData []byte
	doc      Document
}

func (g *board) width(s string, size float64) float64 { return font.MeasureString(g.ttf, s, size) }

// fit обрезает текст по ширине, оставляя многоточие.
func (g *board) fit(s string, maxW, size float64) string {
	if g.width(s, size) <= maxW {
		return s
	}
	runes := []rune(s)
	for len(runes) > 1 {
		runes = runes[:len(runes)-1]
		cand := strings.TrimRight(string(runes), " ") + "…"
		if g.width(cand, size) <= maxW {
			return cand
		}
	}
	return "…"
}

func colorOf(hex uint32) pdf.Color {
	return pdf.RGBHex(hex & 0xFFFFFF)
}

func baseStyle(size float64, hex uint32, align document.TextAlign) document.Style {
	return document.Style{
		FontFamily: "DejaVuSans",
		FontSize:   size,
		Color:      colorOf(hex),
		TextAlign:  align,
		LineHeight: 1.35,
	}
}

func (g *board) text(s string, size float64, hex uint32, align document.TextAlign) *document.Text {
	return &document.Text{Content: s, TextStyle: baseStyle(size, hex, align)}
}

func (g *board) spacer(h float64) *document.Box {
	return &document.Box{BoxStyle: document.BoxStyle{Height: pt(h)}}
}

func bottomBorder(w float64, hex uint32) document.BorderEdges {
	return document.BorderEdges{
		Bottom: document.BorderSide{Width: pt(w), Style: document.BorderSolid, Color: colorOf(hex)},
	}
}

func alignOf(a Align) document.TextAlign {
	switch a {
	case Right:
		return document.AlignRight
	case Center:
		return document.AlignCenter
	}
	return document.AlignLeft
}

// flatten — строку с переносами таблица печатает одной строкой.
func flatten(s string) string {
	return strings.ReplaceAll(s, "\n", " ")
}

// ---------- узлы документа ----------

// metaNode — реквизиты: подпись слева, значения справа, длинные строки переносятся.
func (g *board) metaNode(items []Meta) document.DocumentNode {
	tbl := &document.Table{
		Columns: []document.TableColumn{{Width: pt(mm(40))}, {Width: pt(mm(ContentW - 40))}},
	}
	cellPad := document.Edges{Bottom: pt(2.4)}
	for _, m := range items {
		var values []document.DocumentNode
		for i, line := range m.Lines {
			if i > 0 {
				values = append(values, g.spacer(0.6))
			}
			values = append(values, g.text(flatten(line), sizeMetaValue, hexInk, document.AlignLeft))
		}
		tbl.Body = append(tbl.Body, document.TableRow{Cells: []document.TableCell{
			{
				// Подпись реквизита стоит одной строкой с первым значением.
				Content:   []document.DocumentNode{&document.Text{Content: strings.ToUpper(m.Label), TextStyle: baseStyle(sizeMetaLabel, hexMuted, document.AlignLeft)}},
				CellStyle: document.Style{VerticalAlign: document.VAlignTop, Padding: cellPad},
			},
			{Content: values, CellStyle: document.Style{Padding: cellPad}},
		}})
	}
	return tbl
}

// sectionNodes — заголовок раздела с чертой и таблица данных.
func (g *board) sectionNodes(sec Section) []document.DocumentNode {
	// Заголовок раздела рисуется только когда он задан: пустой Title
	// означает «без жирной черты под названием».
	var head *document.Box
	if sec.Title != "" {
		head = &document.Box{BoxStyle: document.BoxStyle{Width: pt(mm(ContentW))}}
		head.Content = append(head.Content,
			&document.Text{
				Content:   sec.Title,
				TextStyle: document.Style{FontFamily: "DejaVuSans", FontSize: sizeSection, Color: colorOf(hexInk), LineHeight: 1.25},
			},
		)
		// Под названием раздела — линия.
		// Обычная — толстая, под текстом; тонкая (как под строками
		// таблицы) проводится во всю ширину, когда задан PlainTitle.
		if sec.PlainTitle {
			head.Content = append(head.Content,
				&document.Box{
					BoxStyle: document.BoxStyle{Direction: document.DirectionHorizontal, Height: pt(1.2), Border: bottomBorder(0.5, hexRule), Width: pt(mm(ContentW))},
				},
			)
		} else {
			head.Content = append(head.Content,
				&document.Box{
					BoxStyle: document.BoxStyle{Direction: document.DirectionHorizontal, Height: pt(1.2), Border: bottomBorder(0.6, hexRule), Width: pt(mm(ContentW))},
					Content: []document.DocumentNode{
						&document.Box{BoxStyle: document.BoxStyle{Width: pt(g.width(sec.Title, sizeSection)), Border: bottomBorder(1, hexInk)}},
					},
				},
			)
		}
	}

	cols := make([]document.TableColumn, len(sec.Columns))
	aligns := make([]document.TextAlign, len(sec.Columns))
	for i, c := range sec.Columns {
		cols[i] = document.TableColumn{Width: pt(mm(c.Width))}
		aligns[i] = alignOf(c.Align)
	}
	tbl := &document.Table{
		Columns:    cols,
		Header:     []document.TableRow{g.headerRow(sec)},
		TableStyle: document.TableStyle{BoxStyle: document.BoxStyle{Width: pt(mm(ContentW))}},
	}

	pad := document.Edges{Bottom: pt(1.8), Right: pt(1.5)}
	for ri, row := range sec.Rows {
		// Последнюю строку можно подсветить: это строка «К оплате».
		highlight := sec.HighlightLast && ri == len(sec.Rows)-1
		// Shade заливает строку светло-серым — так помечаем неоплаченное.
		shade := highlight || row.Shade
		cells := make([]document.TableCell, 0, len(sec.Columns))
		for j, col := range sec.Columns {
			cell := Cell{}
			if j < len(row.Cells) {
				cell = row.Cells[j]
			}
			hex := uint32(hexInk)
			if cell.Muted {
				hex = hexMuted
			}
			text := g.fit(cell.Text, mm(col.Width-1.5), sizeCell)
			textStyle := document.Style{FontFamily: "DejaVuSans", FontSize: sizeCell, Color: colorOf(hex)}
			if cell.Strong {
				// Сумму итога подчёркиваем: единственный надёжный акцент
				// в PDF, где доступен только обычный начертанием шрифта.
				textStyle.FontWeight = document.WeightBold
				textStyle.TextDecoration = document.DecorationUnderline
			}
			node := &document.Text{Content: text, TextStyle: textStyle}
			style := document.Style{VerticalAlign: document.VAlignBottom, Padding: pad, Border: bottomBorder(0.5, hexRule)}
			if shade {
				bg := colorOf(hexSoft)
				style.Background = &bg
			}
			cells = append(cells, document.TableCell{
				Content:   []document.DocumentNode{node},
				CellStyle: style,
			})
		}
		tbl.Body = append(tbl.Body, document.TableRow{Cells: cells})
	}
	if sec.Total != nil {
		cells := make([]document.TableCell, 0, len(sec.Total))
		for j, c := range sec.Total {
			hex := uint32(hexInk)
			if c.Muted {
				hex = hexMuted
			}
			align := document.AlignLeft
			if j < len(aligns) {
				align = aligns[j]
			}
			cells = append(cells, document.TableCell{
				Content: []document.DocumentNode{g.text(c.Text, sizeCell, hex, align)},
				CellStyle: document.Style{
					Padding: document.Edges{Top: pt(1.6), Bottom: pt(1.2), Right: pt(1.5)},
					Border:  document.BorderEdges{Top: document.BorderSide{Width: pt(0.8), Style: document.BorderSolid, Color: colorOf(hexInk)}},
				},
			})
		}
		tbl.Footer = append(tbl.Footer, document.TableRow{Cells: cells})
	}
	nodes := []document.DocumentNode{g.spacer(9)}
	if head != nil {
		nodes = append(nodes, head, g.spacer(3))
	}
	nodes = append(nodes, tbl)
	return nodes
}

func (g *board) headerRow(sec Section) document.TableRow {
	row := document.TableRow{}
	for _, c := range sec.Columns {
		align := alignOf(c.Align)
		// Заголовок может быть многострочным («расход,\nкВт·ч»). В PDF
		// каждую строку печатаем отдельно — иначе строки слипаются в
		// одну и не влезают в колонку.
		var lines []string
		for _, line := range c.Lines() {
			lines = append(lines, g.fit(flatten(line), mm(c.Width-1.5), sizeHead))
		}
		if len(lines) == 0 {
			lines = []string{""}
		}
		var content []document.DocumentNode
		for k, line := range lines {
			if k > 0 {
				content = append(content, g.spacer(1.2))
			}
			content = append(content, g.text(line, sizeHead, hexMuted, align))
		}
		row.Cells = append(row.Cells, document.TableCell{
			Content: content,
			CellStyle: document.Style{
				VerticalAlign: document.VAlignBottom,
				Padding:       document.Edges{Bottom: pt(2.5), Right: pt(1.5)},
				Border:        bottomBorder(0.8, hexInk),
				TextAlign:     align,
			},
		})
	}
	return row
}

// signatureNode — «Показания сняты…» и строка для подписи.
func (g *board) signatureNode() document.DocumentNode {
	row := &document.Box{BoxStyle: document.BoxStyle{Direction: document.DirectionHorizontal, Width: pt(mm(ContentW))}}
	row.Content = append(row.Content,
		&document.Box{
			BoxStyle: document.BoxStyle{Width: document.Pct(60)},
			Content:  []document.DocumentNode{g.text("Показания сняты «____» ________________ 20____ г.", sizeCell, hexInk, document.AlignLeft)},
		},
		&document.Box{
			BoxStyle: document.BoxStyle{Direction: document.DirectionHorizontal, Width: document.Pct(40)},
			Content: []document.DocumentNode{
				&document.Box{BoxStyle: document.BoxStyle{Width: pt(mm(20))},
					Content: []document.DocumentNode{g.text("Подпись", sizeCell, hexInk, document.AlignLeft)}},
				&document.Box{BoxStyle: document.BoxStyle{Width: pt(mm(28)), Border: bottomBorder(0.6, hexInk)}},
			},
		},
	)
	return &document.Box{BoxStyle: document.BoxStyle{Width: pt(mm(ContentW))}, Content: []document.DocumentNode{g.spacer(12), row}}
}

var _ = model.FormatDateTimeRu

// ---------- раскладка ----------

func (g *board) contentNodes() []document.DocumentNode {
	var nodes []document.DocumentNode
	if app := g.doc.Empty; app != "" {
		nodes = append(nodes, g.text(app, sizeEmpty, hexMuted, document.AlignLeft))
	}
	for _, sec := range g.doc.Sections {
		nodes = append(nodes, g.sectionNodes(sec)...)
	}
	if g.doc.Signature {
		nodes = append(nodes, g.signatureNode())
	}
	return nodes
}

// generate раскладывает содержимое по страницам gpdf, а затем рисует каждую
// страницу: сначала разложенные узлы (шапка документа стоит первым узлом),
// потом шапку продолжения и подвал в координатах листа.
func (g *board) generate() ([]byte, error) {
	var nodes []document.DocumentNode
	nodes = append(nodes, g.headerNodes()...)
	for _, sec := range g.doc.Sections {
		nodes = append(nodes, g.sectionNodes(sec)...)
	}
	if g.doc.Signature {
		nodes = append(nodes, g.signatureNode())
	}

	root := &document.Document{
		Pages: []*document.Page{{
			Size: document.Size{Width: mm(PageW), Height: mm(PageH)},
			Margins: document.Edges{
				Top:    pt(mm(MarginTop)),
				Right:  pt(mm(MarginX)),
				Bottom: pt(mm(FooterHeight)),
				Left:   pt(mm(MarginX)),
			},
			Content: nodes,
		}},
		Metadata: document.DocumentMetadata{
			// Заголовок подставим после сборки: gpdf пишет Info-словарь
			// литеральной строкой, а кириллица в нём должна быть в
			// UTF-16BE — иначе браузер на вкладке показывает иероглифы.
			Title:    pdfTitleMarker,
			Subject:  "MeterZone",
			Creator:  "MeterZone",
			Producer: "MeterZone",
		},
	}
	pages := (&glayout.Paginator{}).Paginate(root)

	var buf bytes.Buffer
	w := pdf.NewWriter(&buf)
	r := grender.NewPDFRenderer(w)
	r.RegisterTTFont("DejaVuSans", g.ttf, g.fontData)
	if err := r.BeginDocument(root.Metadata); err != nil {
		return nil, err
	}
	for i, page := range pages {
		if err := r.BeginPage(page.Size); err != nil {
			return nil, err
		}
		if err := r.RenderNodes(page.Children); err != nil {
			return nil, err
		}
		if err := g.chrome(r, i, len(pages)); err != nil {
			return nil, err
		}
		if err := r.EndPage(); err != nil {
			return nil, err
		}
	}
	if err := r.EndDocument(); err != nil {
		return nil, err
	}
	// Заголовок в Info-словарь подставляем в UTF-16BE: иначе на вкладке
	// браузера вместо названия документа видны иероглифы.
	return patchPDFTitle(buf.Bytes(), g.doc.Title), nil
}

// headerNodes — заголовок, подзаголовок с чертой по его длине и реквизиты.
func (g *board) headerNodes() []document.DocumentNode {
	// Перед шапкой — пустой отступ на разницу между полем листа и
	// положением реквизитов первой строки: gpdf отмерит его сверху.
	var out []document.DocumentNode
	out = append(out, g.spacer(mm(MetaTextY-MarginTop-sizeMetaValue*0.7)))
	title := strings.TrimSpace(g.doc.Title)
	if title == "" {
		title = "Документ"
	}
	out = append(out, &document.Text{
		Content:   title,
		TextStyle: document.Style{FontFamily: "DejaVuSans", FontSize: sizeTitle, Color: colorOf(hexInk), LineHeight: 1.15},
	})
	if sub := strings.TrimSpace(g.doc.Subtitle); sub != "" {
		out = append(out, g.spacer(10), &document.Box{
			BoxStyle: document.BoxStyle{Direction: document.DirectionHorizontal, Width: pt(mm(ContentW))},
			Content: []document.DocumentNode{
				&document.Box{
					BoxStyle: document.BoxStyle{Width: pt(g.width(sub, sizeSubtitle) + 1.5)},
					Content: []document.DocumentNode{
						&document.Text{Content: sub, TextStyle: baseStyle(sizeSubtitle, hexMuted, document.AlignLeft)},
						&document.Box{BoxStyle: document.BoxStyle{Height: pt(1.2), Border: bottomBorder(1, hexInk)}},
					},
				},
			},
		})
	}
	if len(g.doc.Meta) > 0 {
		out = append(out, g.spacer(5), g.metaNode(g.doc.Meta))
	} else {
		out = append(out, g.spacer(9))
	}
	return out
}

// chrome — на страницах-продолжениях шапка «продолжение», на всех —
// дата сборки и «стр. N из M».
func (g *board) chrome(r *grender.PDFRenderer, index, total int) error {
	if index > 0 {
		title := strings.TrimSpace(g.doc.Title)
		rule := colorOf(hexRule)
		if err := r.RenderRect(document.Rectangle{X: mm(MarginX), Y: mm(RunningRuleY - 0.2), Width: mm(ContentW), Height: 0.6 * mmPt}, grender.RectStyle{FillColor: &rule}); err != nil {
			return err
		}
		head := baseStyle(sizeHead, hexMuted, document.AlignLeft)
		if err := r.RenderText(g.fit(title, mm(ContentW/2), sizeHead), document.Point{X: mm(MarginX), Y: mm(RunningTextY) - sizeHead*0.928}, head); err != nil {
			return err
		}
		if err := r.RenderText("продолжение", document.Point{X: mm(MarginX) + mm(ContentW) - g.width("продолжение", sizeHead), Y: mm(RunningTextY) - sizeHead*0.928}, head); err != nil {
			return err
		}
	}
	line := colorOf(hexRule)
	if err := r.RenderRect(document.Rectangle{X: mm(MarginX), Y: mm(FooterRuleY), Width: mm(ContentW), Height: 0.6 * mmPt}, grender.RectStyle{FillColor: &line}); err != nil {
		return err
	}
	foot := baseStyle(sizeFooter, hexMuted, document.AlignLeft)
	stamp := "Документ сформирован автоматически · " + model.FormatDateTimeRu(g.doc.Generated)
	if err := r.RenderText(stamp, document.Point{X: mm(MarginX), Y: mm(FooterTextY) - sizeFooter*0.928}, foot); err != nil {
		return err
	}
	nums := fmt.Sprintf("стр. %d из %d", index+1, total)
	return r.RenderText(nums, document.Point{X: mm(MarginX) + mm(ContentW) - g.width(nums, sizeFooter), Y: mm(FooterTextY) - sizeFooter*0.928}, foot)
}
