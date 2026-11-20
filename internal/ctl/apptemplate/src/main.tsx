import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router";
import { basePath } from "./autable/autable";
import App from "./App";
import "./styles.css";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    {/* The app lives below /apps/<database>/<name>/ (or a preview address);
        basePath() reads it from the <base> tag autable inserts. */}
    <BrowserRouter basename={basePath()}>
      <App />
    </BrowserRouter>
  </StrictMode>,
);
