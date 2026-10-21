// Package docgen собирает печатные документы MeterZone и рисует их.
//
// Документ описывается один раз (Document) и выводится двумя способами:
// PDF через gpdf (pdf.go) и HTML-предпросмотр (шаблон document.html +
// doc.css). Поэтому сетка, кегли и содержание в браузере и в файле совпадают.
package docgen

import (
	"fmt"
	"strings"
	"time"
)

// Align — выравнивание колонки.
type Align uint8

const (
	Left Align = iota
	Right
	Center
)

// Column — колонка таблицы: заголовок (строки через \n) и ширина в мм.
type Column struct {
	Title string
	Width float64
	Align Align
}

// Lines возвращает строки заголовка для многострочной шапки.
func (c Column) Lines() []string { return strings.Split(c.Title, "\n") }

// Class — CSS-класс выравнивания для ячеек колонки.
func (c Column) Class() string {
	switch c.Align {
	case Right:
		return "num"
	case Center:
		return "doc-center"
	}
	return ""
}

// Pct — ширина колонки в процентах от полосы набора (для colgroup).
func (c Column) Pct() string {
	return fmt.Sprintf("%.2f", c.Width/ContentW*100)
}

// Cell — ячейка. Muted приглушает прочерк «—», Strong выделяет
// значение жирным с подчёркиванием (суммы итогов строки).
type Cell struct {
	Text   string
	Muted  bool
	Strong bool
}

// Row — строка таблицы. Shade заливает строку светло-серым:
// так помечаем месяцы, по которым есть неоплаченное.
type Row struct {
	Cells []Cell
	Shade bool
}

// Group — надзаголовок над несколькими колонками (ресурс в ведомости).
type Group struct {
	Title string
	Start int
	Span  int
}

// Section — раздел: заголовок с чертой и таблица.
type Section struct {
	Title   string
	Groups  []Group
	Columns []Column
	Rows    []Row
	Total   []Cell // итоговая строка; nil — без итога

	// RowH — высота строки в мм. Ноль — обычные 7,5 мм; сведения
	// печатаются просторнее, как в образце.
	RowH float64

	// HighlightLast подсвечивает последнюю строку таблицы (итог к оплате).
	HighlightLast bool

	// PlainTitle — заголовок раздела без жирной черты под названием.
	PlainTitle bool
}

func (s Section) rowHeight() float64 {
	if s.RowH > 0 {
		return s.RowH
	}
	return rowH
}

// Meta — строка реквизитов. Несколько Lines печатаются друг под другом.
type Meta struct {
	Label string
	Lines []string
}

// Document — готовый к печати документ.
type Document struct {
	Kind      string // month — сведения, range — ведомость
	Title     string
	Subtitle  string
	Meta      []Meta
	Empty     string
	Sections  []Section
	Signature bool
	Generated time.Time

	// MetaGap и SectionGap — воздух после реквизитов и после таблицы
	// раздела. Ноль означает обычный шаг; ведомость печатается плотнее.
	MetaGap    float64
	SectionGap float64
}

func (d Document) metaGap() float64 {
	if d.MetaGap > 0 {
		return d.MetaGap
	}
	return 17
}

func (d Document) sectionGap() float64 {
	if d.SectionGap > 0 {
		return d.SectionGap
	}
	return 16
}