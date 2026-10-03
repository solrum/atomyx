import { ApiError } from './api.js';
import { bootstrap } from './bootstrap.js';
import { showFatal } from './banners.js';
import { refreshSession } from './session-header.js';

async function start() {
  try {
    await bootstrap();
    await refreshSession();
  } catch (err) {
    showFatal(err instanceof ApiError ? err : new ApiError(0, { message: err.message }));
  }
}

start();
