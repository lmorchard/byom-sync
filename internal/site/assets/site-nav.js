// <byom-site-nav> — renders the shared site navigation from /site-index.json,
// highlighting the current page. Kept dependency-free and self-contained so the
// byom-sync generator can emit it as a static asset (no JS build pipeline).
class ByomSiteNav extends HTMLElement {
  async connectedCallback() {
    try {
      const res = await fetch('/site-index.json');
      const nodes = await res.json();
      const here = location.pathname;
      this.innerHTML = `<nav class="site-nav">${this.render(nodes, here)}</nav>`;
      this.centerActive();
    } catch (e) {
      this.innerHTML = '';
    }
  }
  // Scroll the sidebar so the current page sits as close to vertical center as
  // possible (clamped by the scroll range for items near the top or bottom).
  centerActive() {
    const active = this.querySelector('a[aria-current="page"]');
    const scroller = this.closest('.sidebar');
    if (!active || !scroller) return;
    requestAnimationFrame(() => {
      const a = active.getBoundingClientRect();
      const s = scroller.getBoundingClientRect();
      scroller.scrollTop += a.top - s.top - (scroller.clientHeight - a.height) / 2;
    });
  }
  render(nodes, here) {
    const dirs = nodes.filter((n) => n.isDir);
    const leaves = nodes.filter((n) => !n.isDir);
    let html = '';
    if (dirs.length) {
      html += `<ul>${dirs.map((n) => {
        const active = n.path === here ? ' aria-current="page"' : '';
        const kids = n.children && n.children.length ? this.render(n.children, here) : '';
        return `<li><a href="${esc(n.path)}"${active}>📁 ${esc(n.title)}</a>${kids}</li>`;
      }).join('')}</ul>`;
    }
    let items = '';
    let lastYear = null;
    for (const n of leaves) {
      const y = n.year || 0;
      if (y !== lastYear) {
        items += `<li class="nav-year">${y ? y : 'Undated'}</li>`;
        lastYear = y;
      }
      const active = n.path === here ? ' aria-current="page"' : '';
      const meta = n.meta ? `<span class="nav-meta">${esc(n.meta)}</span>` : '';
      items += `<li><a href="${esc(n.path)}"${active}>${esc(n.title)}</a>${meta}</li>`;
    }
    if (items) html += `<ul>${items}</ul>`;
    return html;
  }
}
function esc(s) { return String(s).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])); }

customElements.define('byom-site-nav', ByomSiteNav);
