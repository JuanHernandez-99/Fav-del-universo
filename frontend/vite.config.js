import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// En desarrollo, Vite reenvía la API al gateway: el navegador ve un solo origen, igual que en producción.
const gateway = { target: 'http://localhost:8080', changeOrigin: true };

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: { '/catalogo': gateway, '/inventario': gateway, '/ventas': gateway, '/usuarios': gateway },
  },
});
