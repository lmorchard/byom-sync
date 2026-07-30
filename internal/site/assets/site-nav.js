// <byom-site-nav> — renders the shared site navigation from /site-index.json,
// highlighting the current page. Kept dependency-free and self-contained so the
// byom-sync generator can emit it as a static asset (no JS build pipeline).
class ByomSiteNav extends HTMLElement {
  async connectedCallback() {
    try {
      const res = await fetch('/site-index.json');
      const data = await res.json();
      // Tolerate the older top-level-array shape (e.g. a stale cached file): the
      // nav still renders, minus the featured group.
      const index = Array.isArray(data) ? { children: data } : data;
      const here = location.pathname;
      this.innerHTML = `<nav class="site-nav">${this.renderFeatured(index.featured, here)}${this.render(index.children || [], here)}</nav>`;
      this.centerActive();
    } catch (e) {
      this.innerHTML = '';
    }
  }
  // Scroll the sidebar so the current page sits as close to vertical center as
  // possible (clamped by the scroll range for items near the top or bottom).
  centerActive() {
    // The current page may appear twice (featured + tree); center on the tree
    // occurrence, falling back to whatever exists.
    const active =
      this.querySelector('ul:not(.nav-featured-list) a[aria-current="page"]') ||
      this.querySelector('a[aria-current="page"]');
    const scroller = this.closest('.sidebar');
    if (!active || !scroller) return;
    requestAnimationFrame(() => {
      const a = active.getBoundingClientRect();
      const s = scroller.getBoundingClientRect();
      scroller.scrollTop += a.top - s.top - (scroller.clientHeight - a.height) / 2;
    });
  }
  // One nav row — shared by the featured group and the year-grouped tree.
  leaf(n, here) {
    const active = n.path === here ? ' aria-current="page"' : '';
    const meta = n.meta ? `<span class="nav-meta">${esc(n.meta)}</span>` : '';
    const cover = n.image ? `<img class="nav-cover" src="${esc(n.image)}" alt="" loading="lazy">` : '';
    return `<li><a class="nav-leaf" href="${esc(n.path)}"${active}>${cover}<span class="nav-text">${esc(n.title)}${meta}</span></a></li>`;
  }
  // The featured group sits at the top of the nav, above the folders and year
  // groups. The list arrives pre-sorted from site-index.json.
  renderFeatured(nodes, here) {
    if (!nodes || !nodes.length) return '';
    const items = nodes.map((n) => this.leaf(n, here)).join('');
    return `<ul class="nav-featured-list"><li class="nav-year nav-featured">Featured</li>${items}</ul>`;
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
      items += this.leaf(n, here);
    }
    if (items) html += `<ul>${items}</ul>`;
    return html;
  }
}
function esc(s) { return String(s).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])); }

customElements.define('byom-site-nav', ByomSiteNav);
