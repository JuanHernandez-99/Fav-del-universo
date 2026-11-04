package dominio

import (
	"errors"
	"testing"
	"time"
)

var ahora = time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

func ventaPendiente() Venta {
	return Venta{ID: "v1", Canal: CanalOnline, Estado: EstadoPendiente, Medio: MedioMercadoPago, Email: "ana@example.com",
		Items: []Item{{VarianteID: "var-remera", Nombre: "Remera", Cantidad: 3, Precio: 1_500_000}}, Total: 4_500_000}
}

func TestCompraExigeEmailMedioEItems(t *testing.T) {
	lineas := []LineaPedido{{"var-remera", 1}}
	casos := map[string]struct {
		clave, email string
		medio        Medio
		lineas       []LineaPedido
		campo        string
	}{
		"invitado sin email (HU-11.2)":  {"clave-valida", "", MedioMercadoPago, lineas, "email"},
		"efectivo no va online (RN-18)": {"clave-valida", "ana@example.com", MedioEfectivo, lineas, "medioPago"},
		"sin clave":                     {"", "ana@example.com", MedioMercadoPago, lineas, "Idempotency-Key"},
		"sin ítems":                     {"clave-valida", "ana@example.com", MedioMercadoPago, nil, "items"},
		"cantidad cero":                 {"clave-valida", "ana@example.com", MedioMercadoPago, []LineaPedido{{"var-remera", 0}}, "items[0].cantidad"},
		"variante repetida":             {"clave-valida", "ana@example.com", MedioMercadoPago, []LineaPedido{{"a", 1}, {"a", 1}}, "items[1].varianteId"},
	}
	for nombre, c := range casos {
		var ev *ErrorValidacion
		if err := ValidarCompra(c.clave, c.email, c.medio, c.lineas); !errors.As(err, &ev) || ev.Campos[0].Campo != c.campo {
			t.Errorf("%s: %v", nombre, err)
		}
	}
	if err := ValidarCompra("clave-valida", "ana@example.com", MedioTransferencia, lineas); err != nil {
		t.Errorf("compra válida rechazada: %v", err)
	}
}

// RN-13: la tienda no tiene máximo de unidades; el contrato con el grupo externo, sí.
func TestLimitesDelPedidoExterno(t *testing.T) {
	once := []LineaPedido{{"var-remera", 11}}
	if err := ValidarPedidoExterno("clave-valida", "", once); !errors.Is(err, ErrPedidoInvalido) {
		t.Errorf("11 unidades en un pedido externo: %v", err)
	}
	if err := ValidarCompra("clave-valida", "ana@example.com", MedioTransferencia, once); err != nil {
		t.Errorf("11 unidades en la tienda: %v", err)
	}
}

func TestVentaLocalExigeMontoYMedio(t *testing.T) {
	lineas := []LineaPedido{{"var-remera", 1}}
	monto := int64(100)
	if err := ValidarVentaLocal("clave-valida", MedioEfectivo, nil, lineas); !errors.Is(err, ErrPedidoInvalido) {
		t.Errorf("sin monto (HU-17.5): %v", err)
	}
	if err := ValidarVentaLocal("clave-valida", Medio("CHEQUE"), &monto, lineas); !errors.Is(err, ErrPedidoInvalido) {
		t.Errorf("medio desconocido (HU-17.4): %v", err)
	}
	if err := ValidarVentaLocal("clave-valida", MedioEfectivo, &monto, lineas); err != nil {
		t.Errorf("venta válida: %v", err)
	}
}

func TestVigenciaSegunElMedio(t *testing.T) {
	if MedioMercadoPago.Vigencia() != 20*time.Minute || MedioTransferencia.Vigencia() != 48*time.Hour {
		t.Error("RN-08 y RN-09: 20 minutos con Mercado Pago, 48 horas con transferencia")
	}
}

func TestHuellaNoDependeDelOrden(t *testing.T) {
	a := HuellaPedido([]string{"x"}, []LineaPedido{{"a", 1}, {"b", 2}})
	b := HuellaPedido([]string{"x"}, []LineaPedido{{"b", 2}, {"a", 1}})
	c := HuellaPedido([]string{"x"}, []LineaPedido{{"a", 2}, {"b", 2}})
	if a != b || a == c {
		t.Error("la huella debe cambiar con el contenido, no con el orden")
	}
}

func TestPagoAprobadoQuedaAConfirmarUnaSolaVez(t *testing.T) {
	v := ventaPendiente()
	if v.RegistrarPago(ahora) != PagoAConfirmar || v.PagoAprobadoEn == nil {
		t.Fatal("el primer pago debe quedar a confirmar")
	}
	if v.Estado != EstadoPendiente {
		t.Errorf("sigue pendiente hasta que Inventario confirme (ADR-005), está en %s", v.Estado)
	}
	if v.RegistrarPago(ahora) != PagoSinEfecto {
		t.Error("el segundo aviso no debe tener efecto (HU-11.6)")
	}
}

func TestPagoConVentaVencidaQuedaAVerificar(t *testing.T) {
	for _, estado := range []Estado{EstadoVencida, EstadoCancelada} {
		v := ventaPendiente()
		v.Estado = estado
		if v.RegistrarPago(ahora) != PagoAVerificar || v.Estado != EstadoAVerificar {
			t.Errorf("desde %s quedó %s (RN-14)", estado, v.Estado)
		}
	}
}

func TestPagoSobreVentaYaPagadaNoTieneEfecto(t *testing.T) {
	v := ventaPendiente()
	v.Estado = EstadoPagada
	if v.RegistrarPago(ahora) != PagoSinEfecto || v.Estado != EstadoPagada {
		t.Errorf("quedó %s (HU-15.3)", v.Estado)
	}
}

func TestConfirmarPagoExigeUnPagoRegistrado(t *testing.T) {
	v := ventaPendiente()
	if err := v.ConfirmarPago(ahora); !errors.Is(err, ErrTransicionInvalida) {
		t.Errorf("sin pago: %v", err)
	}
	v.RegistrarPago(ahora)
	if err := v.ConfirmarPago(ahora); err != nil || v.Estado != EstadoPagada {
		t.Errorf("con pago: %v, estado %s", err, v.Estado)
	}
	if err := v.ConfirmarPago(ahora); err != nil {
		t.Errorf("reintento: %v", err)
	}
}

func TestCancelar(t *testing.T) {
	v := ventaPendiente()
	if cambio, err := v.Cancelar(ahora); err != nil || !cambio || v.Estado != EstadoCancelada {
		t.Errorf("pendiente: %v %v %s", cambio, err, v.Estado)
	}
	if cambio, err := v.Cancelar(ahora); err != nil || cambio {
		t.Errorf("cancelar dos veces no hace nada: %v %v", cambio, err)
	}

	pagada := ventaPendiente()
	pagada.Estado = EstadoPagada
	if _, err := pagada.Cancelar(ahora); !errors.Is(err, ErrTransicionInvalida) {
		t.Errorf("pagada (HU-13.2): %v", err)
	}

	conPago := ventaPendiente()
	conPago.RegistrarPago(ahora)
	if _, err := conPago.Cancelar(ahora); !errors.Is(err, ErrTransicionInvalida) {
		t.Errorf("con pago registrado: %v", err)
	}
}

func TestVencer(t *testing.T) {
	v := ventaPendiente()
	if !v.Vencer(ahora) || v.Estado != EstadoVencida {
		t.Errorf("pendiente quedó %s", v.Estado)
	}
	if v.Vencer(ahora) {
		t.Error("vencer dos veces no hace nada")
	}

	conPago := ventaPendiente()
	conPago.RegistrarPago(ahora)
	if conPago.Vencer(ahora) || conPago.Estado != EstadoPendiente {
		t.Errorf("con pago registrado decide Inventario (ADR-005), quedó %s", conPago.Estado)
	}

	pagada := ventaPendiente()
	pagada.Estado = EstadoPagada
	if pagada.Vencer(ahora) {
		t.Error("una venta pagada no vence")
	}
}

func TestAvanzarSigueElCircuitoSinSaltos(t *testing.T) {
	v := ventaPendiente()
	if err := v.Avanzar(ahora); !errors.Is(err, ErrTransicionInvalida) {
		t.Errorf("pendiente (HU-23.2): %v", err)
	}
	v.Estado = EstadoPagada
	for _, esperado := range []Estado{EstadoEnCorreo, EstadoEntregada} {
		if err := v.Avanzar(ahora); err != nil || v.Estado != esperado {
			t.Fatalf("esperaba %s, quedó %s: %v", esperado, v.Estado, err)
		}
	}
	if err := v.Avanzar(ahora); !errors.Is(err, ErrTransicionInvalida) {
		t.Errorf("entregada: %v", err)
	}
}

func TestMailsSegunLaVenta(t *testing.T) {
	transferencia := ventaPendiente()
	transferencia.Medio = MedioTransferencia
	if m := MailsAlCrear(transferencia, "admin@fav.example"); len(m) != 2 || m[1].Tipo != MailTransferenciaPendiente {
		t.Errorf("transferencia (HU-12.3): %+v", m)
	}
	if m := MailsAlCrear(ventaPendiente(), "admin@fav.example"); len(m) != 1 || m[0].Destinatario != "ana@example.com" {
		t.Errorf("mercado pago: %+v", m)
	}

	externo := transferencia
	externo.Email = ""
	if m := MailsAlCrear(externo, "admin@fav.example"); len(m) != 1 || m[0].Destinatario != "admin@fav.example" {
		t.Errorf("grupo externo sin email: %+v", m)
	}
	if m := MailDespacho(externo); m != nil {
		t.Errorf("sin email no hay despacho: %+v", m)
	}
	if m := MailsAlPagar(ventaPendiente(), "admin@fav.example"); len(m) != 2 {
		t.Errorf("al pagar (HU-20.2, HU-21.1): %+v", m)
	}
}

func TestEsperas(t *testing.T) {
	if EsperaMail(1) != time.Minute || EsperaMail(5) != time.Hour || EsperaMail(9) != time.Hour {
		t.Error("mails: 1, 5, 15, 30 y 60 minutos (ADR-005)")
	}
	if EsperaTarea(1) != 2*time.Second || EsperaTarea(20) != time.Minute {
		t.Error("tareas: crece hasta un minuto")
	}
	if Pesos(1_599_990) != "$15999,90" {
		t.Errorf("Pesos: %s", Pesos(1_599_990))
	}
}
