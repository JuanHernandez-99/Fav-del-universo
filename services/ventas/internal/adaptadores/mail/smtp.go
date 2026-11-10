// Package mail envía los mails por SMTP. En el entorno local el servidor es Mailpit (ADR-014):
// nada sale a internet y los mails se ven en su bandeja web.
package mail

import (
	"context"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/dominio"
	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/observabilidad"
)

const dependencia = "mail"

type SMTP struct {
	direccion string
	remitente string
	timeout   time.Duration
}

func NuevoSMTP(direccion, remitente string, timeout time.Duration) *SMTP {
	return &SMTP{direccion, remitente, timeout}
}

func (s *SMTP) Enviar(ctx context.Context, m dominio.Mail) error {
	err := s.enviar(ctx, m)
	resultado := "ok"
	if err != nil {
		resultado = "error"
	}
	observabilidad.LlamadasDependencia.WithLabelValues(dependencia, resultado).Inc()
	return err
}

func (s *SMTP) enviar(ctx context.Context, m dominio.Mail) error {
	ctx, cancelar := context.WithTimeout(ctx, s.timeout)
	defer cancelar()
	conexion, err := (&net.Dialer{}).DialContext(ctx, "tcp", s.direccion)
	if err != nil {
		return err
	}
	defer conexion.Close()
	if limite, ok := ctx.Deadline(); ok {
		_ = conexion.SetDeadline(limite)
	}
	host, _, _ := net.SplitHostPort(s.direccion)
	cliente, err := smtp.NewClient(conexion, host)
	if err != nil {
		return err
	}
	defer cliente.Close()
	if err := cliente.Mail(s.remitente); err != nil {
		return err
	}
	if err := cliente.Rcpt(m.Destinatario); err != nil {
		return err
	}
	escritor, err := cliente.Data()
	if err != nil {
		return err
	}
	if _, err := escritor.Write(armarMensaje(s.remitente, m)); err != nil {
		return err
	}
	if err := escritor.Close(); err != nil {
		return err
	}
	return cliente.Quit()
}

func armarMensaje(remitente string, m dominio.Mail) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", remitente)
	fmt.Fprintf(&b, "To: %s\r\n", m.Destinatario)
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", m.Asunto))
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n")
	b.WriteString(strings.ReplaceAll(m.Cuerpo, "\n", "\r\n"))
	return []byte(b.String())
}
