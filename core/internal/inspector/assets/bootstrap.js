import { ApiError, exchangeCode } from './api.js';

const FRESH_URL_REMEDY = 'Press Enter in the terminal that runs atomyx inspect to print a fresh URL, then open it.';

// Reads the single-use code from the URL fragment, removes it from the
// address bar, and trades it for the session token. The token lives in
// the api module only, so a reload starts over from a fresh URL.
export async function bootstrap() {
  const code = location.hash.slice(1);
  history.replaceState(null, '', location.pathname + location.search);
  if (!code) {
    throw new ApiError(0, {
      code: 'NO_CODE',
      message: 'This page was opened without a bootstrap code.',
      remedy: FRESH_URL_REMEDY,
    });
  }
  try {
    await exchangeCode(code);
  } catch (err) {
    if (err instanceof ApiError && err.status === 401) {
      throw new ApiError(401, {
        code: err.code,
        message: 'The bootstrap code was refused. It may have expired or already been used.',
        remedy: FRESH_URL_REMEDY,
      });
    }
    throw err;
  }
}
