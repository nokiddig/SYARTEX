// Mock switch. A public build-time variable (only readable in the browser with the NEXT_PUBLIC_ prefix).
// Setting NEXT_PUBLIC_MOCK=1 on Vercel makes the whole site run in mock mode, with no backend needed.
export const MOCK = process.env.NEXT_PUBLIC_MOCK === "1";
