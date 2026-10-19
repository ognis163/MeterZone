package web

import (
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
)

func (s *Server) recoverMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic: %v (%s %s)", rec, r.Method, r.URL.Path)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) securityMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; "+
				"script-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

// localMW пускает только локальные и частные адреса.
func (s *Server) localMW(next http.Handler) http.Handler {
	allowPublic := os.Getenv("ALLOW_PUBLIC") == "1"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if allowPublic {
			next.ServeHTTP(w, r)
			return
		}
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		if !privateIP(host) {
			http.Error(w, "Доступ разрешён только из локальной сети.", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func privateIP(host string) bool {
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil {
		// Например, unix-socket или тестовый адрес.
		return host == "" || host == "localhost"
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

// originMW защищает изменяющие запросы от кросс-сайтовых форм.
func (s *Server) originMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete {
			if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
				http.Error(w, "cross-site request rejected", http.StatusForbidden)
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" {
				if origin == "null" {
					// Некоторые мобильные браузеры присылают Origin: null
					// для обычной отправки формы. Доверяем только same-origin.
					sf := r.Header.Get("Sec-Fetch-Site")
					if sf == "cross-site" || sf == "same-site" {
						http.Error(w, "origin rejected", http.StatusForbidden)
						return
					}
				} else if !sameHost(origin, r.Host) {
					http.Error(w, "origin rejected", http.StatusForbidden)
					return
				}
			} else if ref := r.Header.Get("Referer"); ref != "" {
				if !sameHost(ref, r.Host) {
					http.Error(w, "referer rejected", http.StatusForbidden)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func sameHost(raw, host string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, host)
}

func cacheMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		next.ServeHTTP(w, r)
	})
}
