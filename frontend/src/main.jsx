import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserRouter, Navigate, Route, Routes } from 'react-router';
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
import Panel from './paginas/panel/Panel.jsx';
import PanelVentas from './paginas/panel/PanelVentas.jsx';
import PanelVenta from './paginas/panel/PanelVenta.jsx';
import PanelVentaLocal from './paginas/panel/PanelVentaLocal.jsx';
import PanelProductos from './paginas/panel/PanelProductos.jsx';
import PanelProducto from './paginas/panel/PanelProducto.jsx';
import PanelPromociones from './paginas/panel/PanelPromociones.jsx';
import PanelPromocion from './paginas/panel/PanelPromocion.jsx';
import PanelStock from './paginas/panel/PanelStock.jsx';
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
              <Route path="panel" element={<Panel />}>
                <Route index element={<Navigate to="ventas" replace />} />
                <Route path="ventas" element={<PanelVentas />} />
                <Route path="ventas/:id" element={<PanelVenta />} />
                <Route path="venta-local" element={<PanelVentaLocal />} />
                <Route path="productos" element={<PanelProductos />} />
                <Route path="productos/nuevo" element={<PanelProducto key="nuevo" />} />
                <Route path="productos/:id" element={<PanelProducto key="editar" />} />
                <Route path="promociones" element={<PanelPromociones />} />
                <Route path="promociones/nueva" element={<PanelPromocion key="nueva" />} />
                <Route path="promociones/:id" element={<PanelPromocion key="editar" />} />
                <Route path="stock" element={<PanelStock />} />
              </Route>
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
