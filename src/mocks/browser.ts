// MSW for the browser (dev mock mode). Started lazily from main.tsx when VITE_MOCK is set.
import { setupWorker } from "msw/browser";
import { handlers } from "./handlers";

export const worker = setupWorker(...handlers);
