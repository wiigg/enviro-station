function compactText(value) {
  return value.replace(/\s+/g, " ").trim();
}

export function resolveBackendBaseUrl() {
  const env = import.meta.env.VITE_BACKEND_URL;
  if (env) {
    return env.replace(/\/$/, "");
  }

  if (typeof window === "undefined") {
    return "http://localhost:8080";
  }

  const { hostname, origin, protocol, port } = window.location;
  const isLocalHost = hostname === "localhost" || hostname === "127.0.0.1";
  if (isLocalHost && (port === "5173" || port === "4173")) {
    return `${protocol}//${hostname}:8080`;
  }

  return origin;
}

export function resolveReadAPIKey() {
  const env = import.meta.env.VITE_READ_API_KEY;
  return typeof env === "string" ? env.trim() : "";
}

export function buildReadRequestOptions(signal, readAPIKey = resolveReadAPIKey()) {
  const options = { signal, cache: "no-store" };
  if (readAPIKey) {
    options.headers = { "X-Read-API-Key": readAPIKey };
  }
  return options;
}

export function buildReadStreamUrl(requestUrl, readAPIKey = resolveReadAPIKey()) {
  if (!readAPIKey) {
    return requestUrl;
  }

  const baseUrl =
    typeof window === "undefined" ? "http://localhost" : window.location.origin;
  const url = new URL(requestUrl, baseUrl);
  url.searchParams.set("read_key", readAPIKey);
  return url.toString();
}

export async function parseJSONResponse(response, endpointName, requestUrl, backendBaseUrl) {
  const payloadText = await response.text();
  if (!payloadText) {
    return {};
  }

  try {
    return JSON.parse(payloadText);
  } catch (_error) {
    const compactPayload = compactText(payloadText);
    const contentType = (response.headers.get("content-type") || "").toLowerCase();
    const looksHTML =
      contentType.includes("text/html") ||
      compactPayload.toLowerCase().startsWith("<!doctype html") ||
      compactPayload.toLowerCase().startsWith("<html");

    if (looksHTML) {
      throw new Error(
        `${endpointName} returned HTML. Check VITE_BACKEND_URL (${backendBaseUrl}) and verify ${requestUrl} points to the backend API.`
      );
    }

    const preview = compactPayload.slice(0, 140);
    throw new Error(
      `${endpointName} returned non-JSON${preview ? `: ${preview}` : ""}`
    );
  }
}

export async function fetchEndpointJSON({
  backendBaseUrl,
  endpointName,
  requestUrl,
  signal,
  unavailableMessage,
  warningLabel
}) {
  const response = await fetch(requestUrl, buildReadRequestOptions(signal));
  const payload = await parseJSONResponse(
    response,
    endpointName,
    requestUrl,
    backendBaseUrl
  );

  if (!response.ok) {
    const errorMessage =
      typeof payload.error === "string"
        ? payload.error
        : `${warningLabel.toLowerCase()} request failed with status ${response.status}`;
    console.warn(`${warningLabel} request failed`, {
      status: response.status,
      error: errorMessage
    });
    throw new Error(unavailableMessage);
  }

  return payload;
}
