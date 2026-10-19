package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"indextro/internal/model"
	"indextro/internal/report"
)

type objectListItem struct {
	Object model.Object
	Meters int
	Last   string
	Status report.Status
}

type objectsPageData struct {
	Base
	Items    []objectListItem
	Archived []objectListItem
}

func (s *Server) handleObjects(w http.ResponseWriter, r *http.Request) {
	all, err := s.Store.Objects(true)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "Ошибка", "Не удалось прочитать объекты.")
		return
	}
	base := s.base("Объекты", "objects")
	base.Flash = s.flash(r)
	s.withObjectContext(w, r, &base, 0, "/")
	data := objectsPageData{Base: base}
	for _, o := range all {
		meters, _ := s.Store.Meters(o.ID, false)
		last := ""
		if _, maxP, ok := s.Store.PeriodRange(o.ID); ok {
			if p, err := model.ParsePeriod(maxP); err == nil {
				last = p.ShortLabel()
			}
		}
		item := objectListItem{Object: o, Meters: len(meters), Last: last}
		if od, err := s.loadObjectData(o.ID); err == nil {
			item.Status = report.StatusFor(od, model.PeriodOf(time.Now().In(s.Loc)))
		}
		if o.Archived {
			data.Archived = append(data.Archived, item)
		} else {
			data.Items = append(data.Items, item)
		}
	}
	s.render(w, http.StatusOK, "objects", data)
}

type objectFormData struct {
	Base
	Object        model.Object
	Meters        []model.Meter
	IsNew         bool
	HasData       bool
	Resources     []model.Resource
	ParentOptions []model.Meter
	Meter         model.Meter
}

func (s *Server) handleObjectNew(w http.ResponseWriter, r *http.Request) {
	base := s.base("Новый объект", "objects")
	s.withObjectContext(w, r, &base, 0, "/")
	data := objectFormData{
		Base:  base,
		IsNew: true,
		Object: model.Object{
			Kind: model.KindHouse,
		},
	}
	s.render(w, http.StatusOK, "object_form", data)
}

func (s *Server) handleObjectEdit(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		s.fail(w, http.StatusNotFound, "Объект не найден", "Некорректный адрес объекта.")
		return
	}
	obj, err := s.Store.Object(id)
	if err != nil {
		s.fail(w, http.StatusNotFound, "Объект не найден", "Возможно, он был удалён.")
		return
	}
	meters, _ := s.Store.Meters(id, true)
	_, _, hasData := s.Store.PeriodRange(id)
	base := s.base("Изменить объект", "objects")
	base.Flash = s.flash(r)
	s.withObjectContext(w, r, &base, obj.ID, "/")
	s.render(w, http.StatusOK, "object_form", objectFormData{
		Base:          base,
		Object:        obj,
		Meters:        meters,
		HasData:       hasData,
		Resources:     model.ResourceOrder,
		ParentOptions: activeParentOptions(meters, 0),
	})
}

func validKind(k string) bool {
	switch k {
	case model.KindHouse, model.KindCommercial, model.KindOther:
		return true
	}
	return false
}

func (s *Server) handleObjectSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.fail(w, http.StatusBadRequest, "Ошибка", "Не удалось прочитать форму.")
		return
	}
	id, _ := strconv.ParseInt(r.PostFormValue("id"), 10, 64)
	obj := model.Object{
		ID:            id,
		Name:          strings.TrimSpace(r.PostFormValue("name")),
		Kind:          r.PostFormValue("kind"),
		Address:       strings.TrimSpace(r.PostFormValue("address")),
		Owner:         strings.TrimSpace(r.PostFormValue("owner")),
		AccountWater:  strings.TrimSpace(r.PostFormValue("account_water")),
		AccountEnergy: strings.TrimSpace(r.PostFormValue("account_energy")),
		EmailWater:    strings.TrimSpace(r.PostFormValue("email_water")),
		EmailEnergy:   strings.TrimSpace(r.PostFormValue("email_energy")),
		Note:          strings.TrimSpace(r.PostFormValue("note")),
	}
	if r.PostFormValue("finance_present") != "" {
		obj.FinanceEnabled = r.PostFormValue("finance_enabled") != ""
	} else if id > 0 {
		if old, err := s.Store.Object(id); err == nil {
			obj.FinanceEnabled = old.FinanceEnabled
		}
	}
	if !validKind(obj.Kind) {
		obj.Kind = model.KindHouse
	}
	if r.PostFormValue("archived_present") != "" {
		obj.Archived = r.PostFormValue("archived") != ""
	}
	if obj.Name == "" && obj.Address == "" {
		s.renderObjectError(w, r, obj, "Укажите название или адрес объекта.")
		return
	}
	if len([]rune(obj.Name)) > 120 || len([]rune(obj.Address)) > 300 || len([]rune(obj.Owner)) > 160 {
		s.renderObjectError(w, r, obj, "Слишком длинное название, адрес или собственник.")
		return
	}
	if !validEmail(obj.EmailWater) || !validEmail(obj.EmailEnergy) {
		s.renderObjectError(w, r, obj, "Некорректный адрес почты.")
		return
	}
	if err := s.Store.SaveObject(&obj); err != nil {
		s.renderObjectError(w, r, obj, "Не удалось сохранить объект.")
		return
	}
	if r.PostFormValue("after_save") == "meter" {
		s.redirect(w, r, fmt.Sprintf("/meters/new?object=%d&ok=object", obj.ID))
		return
	}
	s.redirect(w, r, fmt.Sprintf("/objects/%d/edit?ok=object", obj.ID))
}

func (s *Server) renderObjectError(w http.ResponseWriter, r *http.Request, obj model.Object, msg string) {
	meters, _ := s.Store.Meters(obj.ID, true)
	base := s.base("Объект", "objects")
	base.Error = msg
	s.withObjectContext(w, r, &base, obj.ID, "/")
	s.render(w, http.StatusBadRequest, "object_form", objectFormData{
		Base:   base,
		Object: obj,
		Meters: meters,
		IsNew:  obj.ID == 0,
	})
}

func (s *Server) handleObjectArchive(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		s.fail(w, http.StatusNotFound, "Объект не найден", "Некорректный адрес объекта.")
		return
	}
	if err := s.Store.ArchiveObject(id); err != nil {
		s.fail(w, http.StatusInternalServerError, "Ошибка", "Не удалось архивировать объект.")
		return
	}
	s.redirect(w, r, "/objects?ok=archived")
}

func (s *Server) handleObjectDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		s.fail(w, http.StatusNotFound, "Объект не найден", "Некорректный адрес объекта.")
		return
	}
	if err := s.Store.DeleteObject(id); err != nil {
		s.fail(w, http.StatusInternalServerError, "Ошибка", "Не удалось удалить объект.")
		return
	}
	s.redirect(w, r, "/objects?ok=deleted")
}

// ---------------------------------------------------------------- приборы

type meterFormData struct {
	Base
	Meter         model.Meter
	Object        model.Object
	IsNew         bool
	Resources     []model.Resource
	ParentOptions []model.Meter
	NextSort      int
}

func (s *Server) handleMeterNew(w http.ResponseWriter, r *http.Request) {
	objectID, err := strconv.ParseInt(r.URL.Query().Get("object"), 10, 64)
	if err != nil || objectID <= 0 {
		s.fail(w, http.StatusNotFound, "Объект не найден", "Некорректный объект.")
		return
	}
	obj, err := s.Store.Object(objectID)
	if err != nil {
		s.fail(w, http.StatusNotFound, "Объект не найден", "Возможно, он был удалён.")
		return
	}
	meters, _ := s.Store.Meters(objectID, true)
	base := s.base("Новый прибор", "objects")
	s.withObjectContext(w, r, &base, obj.ID, "/")
	s.render(w, http.StatusOK, "meter_form", meterFormData{
		Base:          base,
		Object:        obj,
		IsNew:         true,
		Resources:     model.ResourceOrder,
		ParentOptions: activeParentOptions(meters, 0),
		NextSort:      len(meters) + 1,
		Meter: model.Meter{
			ObjectID:    objectID,
			Resource:    model.WaterCold,
			Sort:        len(meters) + 1,
			InstalledOn: "",
		},
	})
}

func (s *Server) handleMeterEdit(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		s.fail(w, http.StatusNotFound, "Прибор не найден", "Некорректный адрес прибора.")
		return
	}
	m, err := s.Store.Meter(id)
	if err != nil {
		s.fail(w, http.StatusNotFound, "Прибор не найден", "Возможно, он был удалён.")
		return
	}
	obj, err := s.Store.Object(m.ObjectID)
	if err != nil {
		s.fail(w, http.StatusNotFound, "Объект не найден", "Возможно, он был удалён.")
		return
	}
	base := s.base("Изменить прибор", "objects")
	s.withObjectContext(w, r, &base, obj.ID, "/")
	meters, _ := s.Store.Meters(m.ObjectID, true)
	s.render(w, http.StatusOK, "meter_form", meterFormData{
		Base:          base,
		Meter:         m,
		Object:        obj,
		Resources:     model.ResourceOrder,
		ParentOptions: activeParentOptions(meters, m.ID),
	})
}

func (s *Server) handleMeterSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.fail(w, http.StatusBadRequest, "Ошибка", "Не удалось прочитать форму.")
		return
	}
	id, _ := strconv.ParseInt(r.PostFormValue("id"), 10, 64)
	objectID, _ := strconv.ParseInt(r.PostFormValue("object_id"), 10, 64)
	m := model.Meter{
		ID:          id,
		ObjectID:    objectID,
		Resource:    model.Resource(r.PostFormValue("resource")),
		Label:       strings.TrimSpace(r.PostFormValue("label")),
		Brand:       strings.TrimSpace(r.PostFormValue("brand")),
		Serial:      strings.TrimSpace(r.PostFormValue("serial")),
		InstalledOn: strings.TrimSpace(r.PostFormValue("installed_on")),
	}
	m.FinanceMode = r.PostFormValue("finance_mode")
	switch m.FinanceMode {
	case model.FinanceNone, model.FinanceAdvance, model.FinanceBill, model.FinanceBoth:
	default:
		m.FinanceMode = model.FinanceNone
	}
	m.FinanceEnabled = m.FinanceMode != model.FinanceNone
	if !model.ValidResource(string(m.Resource)) {
		s.meterError(w, r, m, "Выберите ресурс.")
		return
	}
	if m.InstalledOn != "" {
		if _, err := parseDate(m.InstalledOn); err != nil {
			s.meterError(w, r, m, "Дата установки должна быть в формате ГГГГ-ММ-ДД.")
			return
		}
	}
	startRaw := strings.TrimSpace(r.PostFormValue("start_value"))
	if startRaw != "" {
		v, err := model.ParseValue(startRaw)
		if err != nil {
			s.meterError(w, r, m, "Начальное показание: "+err.Error())
			return
		}
		m.StartValue = v
	}
	if sortRaw := strings.TrimSpace(r.PostFormValue("sort")); sortRaw != "" {
		if v, err := strconv.Atoi(sortRaw); err == nil {
			m.Sort = v
		}
	}
	meters, _ := s.Store.Meters(objectID, true)
	if raw := strings.TrimSpace(r.PostFormValue("parent_id")); raw != "" && raw != "0" {
		parentID, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parentID <= 0 {
			s.meterError(w, r, m, "Некорректный родительский прибор.")
			return
		}
		var parent *model.Meter
		for i := range meters {
			if meters[i].ID == parentID {
				parent = &meters[i]
				break
			}
		}
		if parent == nil {
			s.meterError(w, r, m, "Родительский прибор не найден.")
			return
		}
		if parent.Archived {
			s.meterError(w, r, m, "Родительский прибор в архиве.")
			return
		}
		if parent.Resource != m.Resource {
			s.meterError(w, r, m, "Дублирующий счётчик должен относиться к тому же ресурсу.")
			return
		}
		if m.ID != 0 && (parentID == m.ID || descendantOf(meters, parentID, m.ID)) {
			s.meterError(w, r, m, "Нельзя выбрать этот же прибор как дублирующий.")
			return
		}
		m.ParentID = parentID
	}
	if m.ID != 0 {
		for _, c := range meters {
			if c.ParentID == m.ID && c.Resource != m.Resource {
				s.meterError(w, r, m, "Сначала смените ресурс у дублирующих счётчиков или отвяжите их.")
				return
			}
		}
	}
	if r.PostFormValue("archived_present") != "" {
		m.Archived = r.PostFormValue("archived") != ""
	} else if m.ID != 0 {
		if old, err := s.Store.Meter(m.ID); err == nil {
			m.Archived = old.Archived
		}
	}
	if _, err := s.Store.Object(objectID); err != nil {
		s.meterError(w, r, m, "Объект не найден.")
		return
	}
	if err := s.Store.SaveMeter(&m); err != nil {
		s.meterError(w, r, m, "Не удалось сохранить прибор.")
		return
	}
	s.redirect(w, r, fmt.Sprintf("/objects/%d/edit?ok=meter", objectID))
}

func parseDate(s string) (time.Time, error) {
	return time.Parse("2006-01-02", s)
}

func (s *Server) meterError(w http.ResponseWriter, r *http.Request, m model.Meter, msg string) {
	obj, err := s.Store.Object(m.ObjectID)
	if err != nil {
		s.fail(w, http.StatusBadRequest, "Ошибка", msg)
		return
	}
	base := s.base("Прибор", "objects")
	base.Error = msg
	s.withObjectContext(w, r, &base, obj.ID, "/")
	meters, _ := s.Store.Meters(m.ObjectID, true)
	s.render(w, http.StatusBadRequest, "meter_form", meterFormData{
		Base:          base,
		Meter:         m,
		Object:        obj,
		Resources:     model.ResourceOrder,
		ParentOptions: activeParentOptions(meters, m.ID),
		IsNew:         m.ID == 0,
	})
}

func (s *Server) handleMeterArchive(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		s.fail(w, http.StatusNotFound, "Прибор не найден", "Некорректный адрес прибора.")
		return
	}
	m, err := s.Store.Meter(id)
	if err != nil {
		s.fail(w, http.StatusNotFound, "Прибор не найден", "Возможно, он был удалён.")
		return
	}
	if err := s.Store.ArchiveMeter(id); err != nil {
		s.fail(w, http.StatusInternalServerError, "Ошибка", "Не удалось архивировать прибор.")
		return
	}
	s.redirect(w, r, fmt.Sprintf("/objects/%d/edit?ok=archived", m.ObjectID))
}

func (s *Server) handleMeterDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		s.fail(w, http.StatusNotFound, "Прибор не найден", "Некорректный адрес прибора.")
		return
	}
	m, err := s.Store.Meter(id)
	if err != nil {
		s.fail(w, http.StatusNotFound, "Прибор не найден", "Возможно, он был удалён.")
		return
	}
	if err := s.Store.DeleteMeter(id); err != nil {
		s.fail(w, http.StatusInternalServerError, "Ошибка", "Не удалось удалить прибор.")
		return
	}
	s.redirect(w, r, fmt.Sprintf("/objects/%d/edit?ok=deleted", m.ObjectID))
}
func descendantOf(meters []model.Meter, candidateID, ancestorID int64) bool {
	byID := map[int64]model.Meter{}
	for _, m := range meters {
		byID[m.ID] = m
	}
	cur := candidateID
	for i := 0; i <= len(meters); i++ {
		m, ok := byID[cur]
		if !ok || m.ParentID == 0 {
			return false
		}
		if m.ParentID == ancestorID {
			return true
		}
		cur = m.ParentID
	}
	return true
}

func activeParentOptions(meters []model.Meter, excludeID int64) []model.Meter {
	out := make([]model.Meter, 0, len(meters))
	for _, m := range meters {
		if m.ID == excludeID || m.Archived {
			continue
		}
		if excludeID != 0 && descendantOf(meters, m.ID, excludeID) {
			continue
		}
		out = append(out, m)
	}
	return out
}
