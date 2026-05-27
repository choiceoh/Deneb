/// <reference types="vite/client" />

// Vite's `?raw` suffix returns the file contents as a string. lucide-static
// SVGs are imported this way so the markup is inlined into the bundle.
declare module '*.svg?raw' {
  const content: string;
  export default content;
}
