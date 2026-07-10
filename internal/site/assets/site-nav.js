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
    } catch (e) {
      this.innerHTML = '';
    }
  }
  render(nodes, here) {
    return `<ul>${nodes.map((n) => {
      const active = n.path === here ? ' aria-current="page"' : '';
      const label = (n.isDir ? '📁 ' : '') + esc(n.title);
      const kids = n.children && n.children.length ? this.render(n.children, here) : '';
      return `<li><a href="${esc(n.path)}"${active}>${label}</a>${kids}</li>`;
    }).join('')}</ul>`;
  }
}
function esc(s) { return String(s).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])); }

customElements.define('byom-site-nav', ByomSiteNav);
