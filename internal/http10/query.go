package http10

import "strings"

// SplitTarget separa path y query string de un target (p. ej., "/path?x=1&y=2").
// No realiza decodificación; eso se agrega si el proyecto lo requiere.
func SplitTarget(t string) (path string, query string) {
	path = t
	if i := strings.IndexByte(t, '?'); i >= 0 {
		path = t[:i]
		query = t[i+1:]
	}
	return
}

// ParseQuery transforma "a=1&b=2" en un mapa simple sin percent-decoding.
// Suficiente para la primera etapa del proyecto.
func ParseQuery(q string) map[string]string {
	if q == "" {
		return map[string]string{}
	}
	m := make(map[string]string)
	for _, kv := range strings.Split(q, "&") {
		if kv == "" {
			continue
		}
		p := strings.SplitN(kv, "=", 2)
		k, v := p[0], ""
		if len(p) == 2 {
			v = p[1]
		}
		m[k] = v
	}
	return m
}
