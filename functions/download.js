export async function onRequest(context) {
  const LATEST_URL = 'https://github.com/wesele/MyProxy/releases/latest/download/qwenportal_linux';

  const resp = await fetch(LATEST_URL);

  const headers = new Headers(resp.headers);
  headers.set('Content-Disposition', 'attachment; filename="qwenportal_linux"');
  headers.set('Cache-Control', 'public, max-age=300');

  return new Response(resp.body, {
    status: resp.status,
    statusText: resp.statusText,
    headers,
  });
}
