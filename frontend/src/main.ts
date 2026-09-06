// Entry point for the doenerstag frontend.
//
// Sprint 4 builds the pipeline, not the application: this file exists so that
// esbuild, Tailwind and the go:embed toggle are proven end to end before
// sprint 10 starts writing screens against them. See
// docs/14_implementation_plan.md.

interface VersionResponse {
  version: string;
  commit: string;
  build_date: string;
}

async function fetchVersion(): Promise<VersionResponse | null> {
  try {
    const response = await fetch("/api/v1/version", {
      headers: { Accept: "application/json" },
    });
    if (!response.ok) {
      return null;
    }
    return (await response.json()) as VersionResponse;
  } catch {
    // The shell must render even when the API does not answer, or a database
    // outage would show a blank page rather than a diagnosis.
    return null;
  }
}

function render(root: HTMLElement, version: VersionResponse | null): void {
  const heading = document.createElement("h1");
  heading.className = "text-2xl font-semibold";
  heading.textContent = "doenerstag";

  const status = document.createElement("p");
  status.className = "mt-2 text-sm opacity-70";
  status.textContent = version
    ? `Server ${version.version} is running. The interface arrives in sprint 10.`
    : "The server is not answering. Check doenerstag server and the database.";

  // textContent throughout, never innerHTML: the CSP forbids inline scripts,
  // and building the DOM explicitly is what keeps that true.
  root.replaceChildren(heading, status);
}

async function main(): Promise<void> {
  const root = document.getElementById("app");
  if (!root) {
    return;
  }
  render(root, await fetchVersion());
}

void main();
