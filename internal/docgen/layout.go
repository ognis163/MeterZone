package docgen

// Геометрия и типографика документа в миллиметрах и пунктах. Те же числа
// заданы в doc.css: предпросмотр в браузере и PDF стоят на одной сетке.
const (
	PageW = 210.0 // A4, мм
	PageH = 297.0

	MarginX      = 22.0
	ContentW     = PageW - 2*MarginX // 166 мм
	MarginTop    = 20.0
	FooterHeight = 10.0 // полоса подвала; тело заканчивается на 20 + 257 = 277 мм

	// Вертикальные ориентиры первой страницы, мм от верхнего края.
	TitleY    = 26.0 // базовая линия заголовка 19 pt
	SubtitleY = 34.0 // базовая линия подзаголовка 11 pt
	RuleY     = 37.3 // линия под подзаголовком
	MetaTextY = 48.2 // базовая линия первой строки реквизитов

	// Страницы продолжения.
	RunningTextY = 16.0
	RunningRuleY = 17.5
	FooterRuleY  = 283.0
	FooterTextY  = 288.0

	rowH       = 7.5 // высота строки таблицы, мм
	metaLineH  = 7.0 // шаг строк реквизитов, мм
	metaLabelH = 4.6 // высота подписи реквизита, мм
	metaWrapH  = 4.6 // шаг переноса длинного значения, мм
)

// Размеры шрифта в пунктах.
const (
	sizeTitle     = 19.0
	sizeSubtitle  = 11.0
	sizeMetaLabel = 7.0
	sizeMetaValue = 10.0
	sizeSection   = 12.5
	sizeHead      = 7.5
	sizeCell      = 9.5
	sizeFooter    = 7.5
	sizeEmpty     = 10.0
)

// mmPt — миллиметры в пункте: толщины линий задаются в пунктах, как в CSS.
const mmPt = 25.4 / 72

// Цвета документа: только чернила, серый текст и волосяные линии.
const (
	hexInk   = 0x111111
	hexMuted = 0x8a8a8a
	hexRule  = 0xd9d9d9
	hexGroup = 0xc4c4c4 // надзаголовок ресурса в ведомости
	hexSoft  = 0xf1f1ec // подсветка строки «К оплате»
)