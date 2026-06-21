async function getVersion() {
  const el = document.getElementById('versionInfo');
  try {
    const r = await fetch('https://api.github.com/repos/wesele/MyProxy/releases/latest');
    if (!r.ok) throw new Error('not found');
    const data = await r.json();
    const size = data.assets?.[0]?.size || 0;
    const sizeMB = (size / 1024 / 1024).toFixed(1);
    const tag = data.tag_name;
    const date = new Date(data.created_at).toLocaleDateString('zh-CN');
    el.innerHTML = `最新版本: <strong>${tag}</strong> (${date}) · ${sizeMB} MB`;
  } catch {
    el.textContent = '获取版本信息失败，请直接下载';
  }
}
getVersion();
