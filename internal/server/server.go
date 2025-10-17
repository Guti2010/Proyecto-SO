package server

import (
	"bufio"
	"net"
	"os"
	"strconv"

	"so-http10-demo/internal/http10"
	"so-http10-demo/internal/router"
	"so-http10-demo/internal/util"
)

// HandleConn procesa exactamente una petición HTTP/1.0 y cierra la conexión.
// Responsabilidades:
//  - Parseo estricto HTTP/1.0 (CRLF obligatorio, versión 1.0).
//  - Trazabilidad vía X-Request-Id y X-Worker-Pid.
//  - Delegación de enrutamiento a la capa router.
//  - Serialización de respuestas con Content-Length y Connection: close.
func HandleConn(c net.Conn) {
	defer c.Close()

	// Identificadores de trazabilidad por respuesta.
	trace := map[string]string{
		"X-Request-Id": util.NewReqID(),
		"X-Worker-Pid": strconv.Itoa(os.Getpid()),
		"Connection":   "close",
	}

	// Parseo de la request (request-line + headers).
	r := bufio.NewReader(c)
	req, err := http10.ParseRequest(r)
	if err != nil {
		// Errores de sintaxis se reportan como 400 con JSON uniforme.
		http10.WriteErrorJSON(c, 400, "bad_request", err.Error(), trace)
		return
	}

	// Enrutamiento y serialización.
	res := router.Dispatch(req.Method, req.Target)
	if res.JSON {
		if res.Err != nil {
			http10.WriteErrorJSON(c, res.Status, res.Err.Code, res.Err.Detail, trace)
		} else {
			http10.WriteJSONH(c, res.Status, res.Body, trace)
		}
	} else {
		http10.WritePlainH(c, res.Status, res.Body, trace)
	}
}

// ListenAndServe abre un listener TCP en addr y atiende conexiones
// lanzando una goroutine por cada cliente. Bloquea hasta error fatal.
func ListenAndServe(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer ln.Close()

	for {
		conn, err := ln.Accept()
		if err != nil {
			// Si necesitas tratar errores temporales, puedes envolver aquí.
			// Con lo mínimo, devolvemos el error para que el proceso falle
			// y el orquestador (docker) lo reinicie si corresponde.
			return err
		}
		go HandleConn(conn)
	}
}