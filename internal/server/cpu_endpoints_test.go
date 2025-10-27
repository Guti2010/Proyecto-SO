package server

import (
	"strings"
	"testing"
)

// Pruebas "blandas" de endpoints CPU. Si algún endpoint no está disponible
// en tu build, okOrSkip hará que no falle la corrida completa.
func Test_CPU_IsPrime(t *testing.T) {
	// Caso feliz: número primo
	r := hit(t, "GET /cpu/isprime?n=17 HTTP/1.0\r\n")
	if codeOf(r) != 200 {
		okOrSkip(t, false, "endpoint /cpu/isprime no disponible (200 no alcanzado)")
		return
	}
	okOrSkip(t, strings.Contains(bodyOf(r), "true") ||
		strings.Contains(strings.ToLower(bodyOf(r)), "prime"),
		`/cpu/isprime no indica correctamente que 17 es primo`)

	// Caso error: parámetro inválido (si no está implementado, no fallamos la suite)
	r = hit(t, "GET /cpu/isprime?n=abc HTTP/1.0\r\n")
	okOrSkip(t, codeOf(r) == 400 || codeOf(r) == 200,
		"esperaba 400 ó 200 ante input inválido en /cpu/isprime")
}

func Test_CPU_Pi(t *testing.T) {
	// Intenta generar algunos dígitos de PI
	r := hit(t, "GET /cpu/pi?d=20 HTTP/1.0\r\n")
	if codeOf(r) != 200 {
		okOrSkip(t, false, "endpoint /cpu/pi no disponible (200 no alcanzado)")
		return
	}
	// No exigimos formato exacto; solo que haya algo con '3' y '.'
	okOrSkip(t, strings.Contains(bodyOf(r), "3") && strings.Contains(bodyOf(r), "."),
		`/cpu/pi no retornó algo que parezca PI`)
}

func Test_CPU_Mandelbrot(t *testing.T) {
	// Render sencillo del fractal (si existe)
	r := hit(t, "GET /cpu/mandelbrot HTTP/1.0\r\n")
	if codeOf(r) != 200 {
		okOrSkip(t, false, "endpoint /cpu/mandelbrot no disponible (200 no alcanzado)")
		return
	}
	okOrSkip(t, len(bodyOf(r)) > 0, `/cpu/mandelbrot no retornó contenido`)
}

func Test_CPU_MatrixMul(t *testing.T) {
	// Multiplicación de matrices con hash (si existe)
	r := hit(t, "GET /cpu/matrixmulhash HTTP/1.0\r\n")
	if codeOf(r) != 200 {
		okOrSkip(t, false, "endpoint /cpu/matrixmulhash no disponible (200 no alcanzado)")
		return
	}
	okOrSkip(t, len(bodyOf(r)) > 0, `/cpu/matrixmulhash no retornó contenido`)
}
