// The codes are the contract with backend/pkg/growth/connectors/browser.go.
export const STATUS = {
  bad_request: 400,
  unauthorized: 401,
  not_found: 404,
  needs_login: 423,
  rate_limited: 429,
  unavailable: 502,
  timeout: 504,
  internal: 500,
};

export class ServiceError extends Error {
  constructor(code, message, extra = {}) {
    super(message);
    this.code = code;
    Object.assign(this, extra);
  }
}

export function toServiceError(err) {
  if (err instanceof ServiceError) return err;
  if (err && err.name === 'TimeoutError') return new ServiceError('timeout', err.message);
  if (err && err.code && STATUS[err.code]) return new ServiceError(err.code, err.message);
  return new ServiceError('internal', err && err.message ? err.message : String(err));
}
