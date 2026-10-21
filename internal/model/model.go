package model

import (
	"strings"
	"time"
)

// Object — учитываемое помещение: дом, коммерческий объект и т. п.
type Object struct {
	ID             int64
	Name           string
	Kind           string
	Address        string
	Owner          string
	AccountWater   string
	AccountEnergy  string
	EmailWater     string
	EmailEnergy    string
	FinanceEnabled bool
	Note           string
	Archived       bool
	CreatedAt      time.Time
}

const (
	KindHouse      = "house"
	KindCommercial = "commercial"
	KindOther      = "other"
)

// KindLabel возвращает человекочитаемый тип объекта.
func KindLabel(kind string) string {
	switch kind {
	case KindCommercial:
		return "Коммерческий"
	case KindOther:
		return "Другой"
	default:
		return "Дом"
	}
}

// Title — краткое название для списков и заголовков.
func (o Object) Title() string {
	if o.Name != "" {
		return o.Name
	}
	if o.Address != "" {
		return o.Address
	}
	return "Объект"
}

// Meter — прибор учёта, установленный на объекте.
type Meter struct {
	ID             int64
	ObjectID       int64
	Resource       Resource
	Label          string // место установки: кухня, подвал, щиток…
	Brand          string
	Serial         string
	StartValue     float64 // показание на момент установки
	InstalledOn    string  // YYYY-MM-DD, необязательно
	ParentID       int64   // 0 — самостоятельный прибор, иначе суб-счётчик
	FinanceEnabled bool    // вести аванс и счёт по этому прибору
	FinanceMode    string  // none, advance, bill, both
	Archived       bool
	Sort           int

	// PreviousSerial — заводской номер, который был до замены счётчика,
	// ReplacedAt — месяц замены (YYYY-MM), с которого идёт новый отсчёт.
	PreviousSerial string
	ReplacedAt     string
}

// SerialChanged — заводской номер сменился. С этого месяца счётчик новый,
// поэтому показания могут быть меньше предыдущих.
func (m Meter) SerialChanged(prev string) bool {
	cur := strings.TrimSpace(m.Serial)
	old := strings.TrimSpace(prev)
	return cur != "" && old != "" && !strings.EqualFold(cur, old)
}

// DetailShort — марка и заводской номер без места установки.
func (m Meter) DetailShort() string {
	switch {
	case m.Brand != "" && m.Serial != "":
		return m.Brand + " — " + m.Serial
	case m.Brand != "":
		return m.Brand
	case m.Serial != "":
		return m.Serial
	}
	return ""
}

// LabelShort — «ХВС Бетар-м №9499449» для ведомости.
func (m Meter) LabelShort() string {
	parts := []string{m.Resource.Short()}
	if m.Brand != "" {
		parts = append(parts, m.Brand)
	}
	if m.Serial != "" {
		parts = append(parts, "№"+m.Serial)
	}
	if len(parts) == 1 && m.Label != "" {
		parts = append(parts, m.Label)
	}
	return strings.Join(parts, " ")
}

const (
	FinanceNone    = "none"
	FinanceAdvance = "advance"
	FinanceBill    = "bill"
	FinanceBoth    = "both"
)

// FinanceModeValue возвращает режим финансового учёта прибора.
func (m Meter) FinanceModeValue() string {
	if m.FinanceMode != "" {
		return m.FinanceMode
	}
	if m.FinanceEnabled {
		return FinanceBoth
	}
	return FinanceNone
}

// ShowAdvance сообщает, нужно ли показывать аванс.
func (m Meter) ShowAdvance() bool {
	mode := m.FinanceModeValue()
	return mode == FinanceAdvance || mode == FinanceBoth
}

// ShowBill сообщает, нужно ли показывать счёт.
func (m Meter) ShowBill() bool {
	mode := m.FinanceModeValue()
	return mode == FinanceBill || mode == FinanceBoth
}

// IsSub сообщает, что прибор является суб-счётчиком.
func (m Meter) IsSub() bool { return m.ParentID > 0 }

// Title — название прибора в интерфейсе и документах.
func (m Meter) Title() string {
	if m.Label != "" {
		return m.Label
	}
	if m.Brand != "" {
		return m.Brand
	}
	return m.Resource.Label()
}

// Detail — марка и заводской номер одной строкой.
func (m Meter) Detail() string {
	switch {
	case m.Brand != "" && m.Serial != "":
		return m.Brand + " · " + m.Serial
	case m.Brand != "":
		return m.Brand
	case m.Serial != "":
		return m.Serial
	}
	return ""
}

// Reading — показание прибора за конкретный месяц.
type Reading struct {
	MeterID   int64
	Period    string
	Value     float64
	Note      string
	UpdatedAt time.Time
}

// Resource — вид ресурса. Значения стабильны: они хранятся в базе.
type Resource string

const (
	WaterCold Resource = "water_cold"
	WaterHot  Resource = "water_hot"
	Energy    Resource = "energy"
)

// ResourceOrder — порядок отображения ресурсов.
var ResourceOrder = []Resource{WaterCold, WaterHot, Energy}

// ValidResource проверяет, что ресурс поддерживается.
func ValidResource(v string) bool {
	switch Resource(v) {
	case WaterCold, WaterHot, Energy:
		return true
	}
	return false
}

// Label — полное название ресурса.
func (r Resource) Label() string {
	switch r {
	case WaterCold:
		return "Холодная вода"
	case WaterHot:
		return "Горячая вода"
	case Energy:
		return "Электроэнергия"
	}
	return string(r)
}

// Short — короткое обозначение для таблиц.
func (r Resource) Short() string {
	switch r {
	case WaterCold:
		return "ХВС"
	case WaterHot:
		return "ГВС"
	case Energy:
		return "Электроэнергия"
	}
	return string(r)
}

// Section — заголовок раздела документа.
func (r Resource) Section() string {
	switch r {
	case WaterCold:
		return "Холодное водоснабжение"
	case WaterHot:
		return "Горячее водоснабжение"
	case Energy:
		return "Электроэнергия"
	}
	return string(r)
}

// Unit — единица измерения.
func (r Resource) Unit() string {
	if r == Energy {
		return "кВт·ч"
	}
	return "куб.м"
}

// IsWater сообщает, относится ли ресурс к водоснабжению.
func (r Resource) IsWater() bool { return r == WaterCold || r == WaterHot }

// IsEnergy сообщает, что ресурс — электроэнергия.
func (r Resource) IsEnergy() bool { return r == Energy }
