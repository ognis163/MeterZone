package web

import (
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"indextro/internal/model"
)

const objectCookie = "indextro-object"

// selectedObjectID возвращает объект из query, cookie или первый доступный.
func (s *Server) selectedObjectID(r *http.Request) int64 {
	if raw := strings.TrimSpace(r.URL.Query().Get("object")); raw != "" {
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil {
			if _, err := s.Store.Object(id); err == nil {
				return id
			}
		}
	}
	if c, err := r.Cookie(objectCookie); err == nil {
		if id, err := strconv.ParseInt(strings.TrimSpace(c.Value), 10, 64); err == nil {
			if _, err := s.Store.Object(id); err == nil {
				return id
			}
		}
	}
	if objs, err := s.Store.Objects(false); err == nil && len(objs) > 0 {
		return firstObjectID(objs)
	}
	return 0
}

func firstObjectID(objs []model.Object) int64 {
	if len(objs) == 0 {
		return 0
	}
	best := objs[0]
	for _, o := range objs[1:] {
		if o.ID < best.ID {
			best = o
		}
	}
	return best.ID
}

// withObjectContext заполняет общий переключатель объекта в шапке.
func (s *Server) withObjectContext(w http.ResponseWriter, r *http.Request, base *Base,
	preferred int64, action string) {

	if action == "" {
		action = "/"
	}
	base.ObjectSwitchAction = action
	q := r.URL.Query()
	q.Del("object")
	q.Del("ok")
	q.Del("err")
	if enc := q.Encode(); enc != "" {
		base.ObjectSwitchQuery = template.URL("&" + enc)
	}
	objects, err := s.Store.Objects(false)
	if err != nil {
		return
	}
	base.Objects = objects

	id := preferred
	if raw := strings.TrimSpace(r.URL.Query().Get("object")); raw != "" {
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil {
			if _, err := s.Store.Object(v); err == nil {
				id = v
			}
		}
	}
	if id == 0 {
		if c, err := r.Cookie(objectCookie); err == nil {
			if v, err := strconv.ParseInt(strings.TrimSpace(c.Value), 10, 64); err == nil {
				if _, err := s.Store.Object(v); err == nil {
					id = v
				}
			}
		}
	}
	if id == 0 && len(objects) > 0 {
		id = firstObjectID(objects)
	}
	if id == 0 {
		return
	}
	obj, err := s.Store.Object(id)
	if err != nil {
		return
	}
	base.ObjectID = obj.ID
	base.ObjectName = obj.Title()

	// Архивный объект тоже должен быть виден в переключателе, если он открыт.
	found := false
	for _, o := range base.Objects {
		if o.ID == obj.ID {
			found = true
			break
		}
	}
	if !found {
		base.Objects = append([]model.Object{obj}, base.Objects...)
	}
	if w != nil {
		http.SetCookie(w, &http.Cookie{
			Name:     objectCookie,
			Value:    strconv.FormatInt(obj.ID, 10),
			Path:     "/",
			MaxAge:   365 * 24 * 60 * 60,
			SameSite: http.SameSiteLaxMode,
		})
	}
}

// formValueFrom читает поля формы из обычного и многочастного запроса
// (нужно для загрузки файлов в формах объектов и настроек).
func formValueFrom(r *http.Request) func(string) string {
	return func(name string) string {
		if r.MultipartForm != nil {
			if vs, ok := r.MultipartForm.Value[name]; ok && len(vs) > 0 {
				return vs[0]
			}
			return ""
		}
		return r.PostFormValue(name)
	}
}

const periodCookie = "indextro-period"

// periodContext возвращает выбранный месяц: из адреса, из cookie или текущий.
func (s *Server) periodContext(r *http.Request) model.Period {
	today := model.PeriodOf(time.Now().In(s.Loc))
	for _, raw := range []string{r.URL.Query().Get("m"), r.FormValue("m")} {
		if raw == "" {
			continue
		}
		if p, err := model.ParsePeriod(raw); err == nil {
			return p
		}
	}
	if c, err := r.Cookie(periodCookie); err == nil {
		if p, err := model.ParsePeriod(strings.TrimSpace(c.Value)); err == nil {
			return p
		}
	}
	return today
}

// rememberPeriod запоминает месяц, чтобы он переходил между страницами.
func rememberPeriod(w http.ResponseWriter, p model.Period) {
	if w == nil || !p.Valid() {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     periodCookie,
		Value:    p.String(),
		Path:     "/",
		MaxAge:   365 * 24 * 60 * 60,
		SameSite: http.SameSiteLaxMode,
	})
}
