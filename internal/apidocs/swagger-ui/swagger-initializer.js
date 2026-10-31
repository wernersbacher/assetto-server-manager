window.onload = function () {
  // requests of "Try it out" are same-origin, so they use the session cookie of the logged in account
  window.ui = SwaggerUIBundle({
    url: "/api/openapi.yaml",
    dom_id: "#swagger-ui",
    deepLinking: true
  });
};
