// Separate file rather than an inline block: the CSP forbids inline scripts,
// and Swagger UI is served under the same policy as everything else.
window.SwaggerUIBundle({
  url: "/api/v1/openapi.json",
  dom_id: "#swagger-ui",
  deepLinking: true,
  // No "try it out" against another server: the document describes this one.
  supportedSubmitMethods: ["get"],
});
