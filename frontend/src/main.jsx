import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserRouter, Route, Routes } from 'react-router';
import '@fontsource-variable/fraunces';
import '@fontsource-variable/manrope';
import './estilos.css';
import { ProveedorCarrito } from './carrito/contexto.jsx';
import { ProveedorSesion } from './sesion/contexto.jsx';
import Marco from './componentes/Marco.jsx';
import Tienda from './paginas/Tienda.jsx';
import Producto from './paginas/Producto.jsx';
import Carrito from './paginas/Carrito.jsx';
import Acceso from './paginas/Acceso.jsx';
import Comprar from './paginas/Comprar.jsx';
import CompraResultado from './paginas/CompraResultado.jsx';
import MisCompras from './paginas/MisCompras.jsx';
import NoEncontrada from './paginas/NoEncontrada.jsx';

createRoot(document.getElementById('raiz')).render(
  <StrictMode>
    <BrowserRouter>
      <ProveedorSesion>
        <ProveedorCarrito>
          <Routes>
            <Route element={<Marco />}>
              <Route index element={<Tienda />} />
              <Route path="producto/:id" element={<Producto />} />
              <Route path="carrito" element={<Carrito />} />
              <Route path="comprar" element={<Comprar />} />
              <Route path="compra/:id" element={<CompraResultado />} />
              <Route path="mis-compras" element={<MisCompras />} />
              <Route path="ingresar" element={<Acceso key="ingresar" modo="ingresar" />} />
              <Route path="registrarme" element={<Acceso key="registrar" modo="registrar" />} />
              <Route path="*" element={<NoEncontrada />} />
            </Route>
          </Routes>
        </ProveedorCarrito>
      </ProveedorSesion>
    </BrowserRouter>
  </StrictMode>,
);
