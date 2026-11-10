package cache

import (
	"bufio"
	"net"
	"sync"
	"testing"
	"time"
)

// memcachedFalso responde que ninguna clave existe, y puede cortar todas sus conexiones, como un reinicio.
type memcachedFalso struct {
	escucha    net.Listener
	mu         sync.Mutex
	conexiones []net.Conn
}

func nuevoMemcachedFalso(t *testing.T) *memcachedFalso {
	t.Helper()
	escucha, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &memcachedFalso{escucha: escucha}
	t.Cleanup(func() { _ = escucha.Close(); f.cortar() })
	go func() {
		for {
			conexion, err := escucha.Accept()
			if err != nil {
				return
			}
			f.mu.Lock()
			f.conexiones = append(f.conexiones, conexion)
			f.mu.Unlock()
			go func() {
				lector := bufio.NewScanner(conexion)
				for lector.Scan() {
					time.Sleep(5 * time.Millisecond) // para que los pedidos simultáneos abran conexiones distintas
					_, _ = conexion.Write([]byte("END\r\n"))
				}
			}()
		}
	}()
	return f
}

func (f *memcachedFalso) cortar() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, conexion := range f.conexiones {
		_ = conexion.Close()
	}
	f.conexiones = nil
}

// Tras un reinicio de Memcached, un solo error alcanza: las demás conexiones en espera también estaban muertas.
func TestTrasUnReinicioNoQuedanConexionesMuertas(t *testing.T) {
	f := nuevoMemcachedFalso(t)
	c := Nuevo(f.escucha.Addr().String())
	defer c.Cerrar()

	var grupo sync.WaitGroup
	for range 10 {
		grupo.Go(func() {
			if _, _, err := c.Leer(ctx, "remera"); err != nil {
				t.Error(err)
			}
		})
	}
	grupo.Wait()

	f.cortar()
	time.Sleep(20 * time.Millisecond)

	errores := 0
	for range 10 {
		if _, _, err := c.Leer(ctx, "remera"); err != nil {
			errores++
		}
	}
	if errores > 1 {
		t.Fatalf("%d lecturas fallaron tras el reinicio: se esperaba a lo sumo 1", errores)
	}
}
