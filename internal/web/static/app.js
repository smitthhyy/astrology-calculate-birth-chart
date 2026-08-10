// Progressive enhancement only. Every field is a plain input that submits and
// validates server-side without this file; the script adds the searchable
// dropdowns, the theme toggle and the PNG and PDF exports.

(() => {
  'use strict';

  // ---------------------------------------------------------------- theme

  const THEME_KEY = 'birthchart-theme';

  function applyTheme(theme) {
    const icon = document.querySelector('[data-theme-icon]');
    if (theme) {
      document.documentElement.dataset.theme = theme;
    } else {
      delete document.documentElement.dataset.theme;
    }
    if (icon) {
      const dark = theme === 'dark' ||
        (!theme && window.matchMedia('(prefers-color-scheme: dark)').matches);
      icon.textContent = dark ? '☀︎' : '☽︎';
    }
  }

  applyTheme(localStorage.getItem(THEME_KEY));

  document.querySelector('[data-theme-toggle]')?.addEventListener('click', () => {
    const dark = document.documentElement.dataset.theme === 'dark' ||
      (!document.documentElement.dataset.theme &&
        window.matchMedia('(prefers-color-scheme: dark)').matches);
    const next = dark ? 'light' : 'dark';
    localStorage.setItem(THEME_KEY, next);
    applyTheme(next);
  });

  // ------------------------------------------------------------- combobox

  // Combobox wires an input to a listbox of suggestions. `source` is an async
  // function returning [{value, label, sub, data}], and `onPick` is called
  // with the chosen entry.
  function combobox(root, { source, onPick, minChars = 1, openOnFocus = false }) {
    const input = root.querySelector('input[role="combobox"]');
    const list = root.querySelector('.combo-list');
    if (!input || !list) return;

    let items = [];
    let active = -1;
    let sequence = 0;

    const close = () => {
      list.hidden = true;
      list.replaceChildren();
      input.setAttribute('aria-expanded', 'false');
      active = -1;
    };

    const setActive = (index) => {
      const options = [...list.querySelectorAll('li[role="option"]')];
      options.forEach((li, i) => li.setAttribute('aria-selected', String(i === index)));
      active = index;
      options[index]?.scrollIntoView({ block: 'nearest' });
    };

    const show = (results, emptyMessage) => {
      items = results;
      list.replaceChildren();

      if (!results.length) {
        const li = document.createElement('li');
        li.className = 'combo-empty';
        li.textContent = emptyMessage || 'No matches';
        list.append(li);
      } else {
        results.forEach((item, i) => {
          const li = document.createElement('li');
          li.setAttribute('role', 'option');
          li.setAttribute('aria-selected', 'false');
          li.id = `${list.id}-opt-${i}`;

          li.append(document.createTextNode(item.label));
          if (item.sub) {
            const sub = document.createElement('span');
            sub.className = 'combo-sub';
            sub.textContent = item.sub;
            li.append(sub);
          }
          // mousedown rather than click: blur would close the list first.
          li.addEventListener('mousedown', (e) => {
            e.preventDefault();
            choose(i);
          });
          list.append(li);
        });
      }
      list.hidden = false;
      input.setAttribute('aria-expanded', 'true');
      setActive(results.length ? 0 : -1);
    };

    const choose = (index) => {
      const item = items[index];
      if (!item) return;
      onPick(item);
      close();
    };

    const search = async () => {
      const query = input.value.trim();
      if (query.length < minChars) {
        close();
        return;
      }
      const mine = ++sequence;
      try {
        const results = await source(query);
        if (mine !== sequence) return; // a later keystroke already won
        show(results);
      } catch (err) {
        if (mine === sequence) show([], 'The search failed. Try again.');
      }
    };

    let timer;
    input.addEventListener('input', () => {
      clearTimeout(timer);
      timer = setTimeout(search, 140);
    });

    if (openOnFocus) {
      input.addEventListener('focus', search);
    }

    input.addEventListener('keydown', (e) => {
      if (list.hidden && (e.key === 'ArrowDown' || e.key === 'ArrowUp')) {
        search();
        return;
      }
      if (list.hidden) return;

      const count = list.querySelectorAll('li[role="option"]').length;
      switch (e.key) {
        case 'ArrowDown':
          e.preventDefault();
          if (count) setActive((active + 1) % count);
          break;
        case 'ArrowUp':
          e.preventDefault();
          if (count) setActive((active - 1 + count) % count);
          break;
        case 'Enter':
          if (active >= 0) {
            e.preventDefault();
            choose(active);
          }
          break;
        case 'Escape':
          close();
          break;
      }
    });

    input.addEventListener('blur', () => setTimeout(close, 120));
    return { search };
  }

  // ------------------------------------------------------- place searching

  const placeRoot = document.querySelector('[data-combo="places"]');
  if (placeRoot) {
    const country = document.getElementById('country');
    const zoneField = document.getElementById('zone');
    const zoneSearch = document.getElementById('zone-search');
    const zoneCurrent = document.querySelector('[data-zone-current]');
    const latitude = document.getElementById('latitude');
    const longitude = document.getElementById('longitude');

    let endpoint = placeRoot.dataset.endpoint || '/api/places';

    const fetchPlaces = async (query) => {
      const res = await fetch(`${endpoint}?q=${encodeURIComponent(query)}`);
      const body = await res.json();
      if (!res.ok) throw new Error(body.error || 'lookup failed');
      return (body.results || []).map((p) => ({
        label: p.label,
        sub: `${p.timezone} · ${p.latitude.toFixed(3)}, ${p.longitude.toFixed(3)}`,
        data: p,
      }));
    };

    // Whether the coordinates now in the boxes were copied from a picked place
    // or typed by the user. Only the former may be thrown away.
    let coordsFromPicker = false;

    const places = combobox(placeRoot, {
      source: fetchPlaces,
      onPick: ({ data }) => {
        placeRoot.querySelector('input').value = data.name;
        if (country) country.value = data.country || '';
        if (latitude) latitude.value = data.latitude.toFixed(4);
        if (longitude) longitude.value = data.longitude.toFixed(4);
        coordsFromPicker = true;
        if (zoneField && data.timezone) {
          zoneField.value = data.timezone;
          if (zoneCurrent) zoneCurrent.textContent = data.timezone;
          if (zoneSearch) zoneSearch.value = '';
        }
      },
    });

    // Editing either coordinate makes it the user's own, and no longer the last
    // picked place's to discard.
    [latitude, longitude].forEach((field) => {
      field?.addEventListener('input', () => { coordsFromPicker = false; });
    });

    // Typing a new place invalidates coordinates copied from the last one — but
    // not coordinates typed by hand, which are the answer to the place not
    // being in the gazetteer at all. Clearing those on the next keystroke in
    // the place field made that answer impossible to give: the form came back
    // saying the place did not exist, having just discarded the coordinates
    // that said where it was.
    placeRoot.querySelector('input')?.addEventListener('input', () => {
      if (!coordsFromPicker) return;
      if (latitude) latitude.value = '';
      if (longitude) longitude.value = '';
      coordsFromPicker = false;
    });

    document.querySelector('[data-online-search]')?.addEventListener('click', () => {
      endpoint = '/api/geocode';
      placeRoot.querySelector('input')?.focus();
      places?.search();
    });
  }

  // -------------------------------------------------------- zone searching

  const zoneRoot = document.querySelector('[data-combo="zones"]');
  if (zoneRoot) {
    const zoneField = document.getElementById('zone');
    const zoneCurrent = document.querySelector('[data-zone-current]');

    // The datalist the page already renders is the source, so the zone list
    // is not sent twice.
    const zones = [...document.querySelectorAll('#zone-options option')].map((o) => ({
      name: o.value,
      label: o.label || o.value,
    }));

    combobox(zoneRoot, {
      minChars: 0,
      openOnFocus: true,
      source: async (query) => {
        const needle = query.toLowerCase().replace(/\s+/g, '_');
        return zones
          .filter((z) => !needle || z.name.toLowerCase().includes(needle) ||
            z.label.toLowerCase().includes(query.toLowerCase()))
          .slice(0, 40)
          .map((z) => ({ label: z.label, data: z }));
      },
      onPick: ({ data }) => {
        if (zoneField) zoneField.value = data.name;
        if (zoneCurrent) zoneCurrent.textContent = data.name;
        zoneRoot.querySelector('input').value = '';
      },
    });
  }

  // ----------------------------------------------------------- PNG export

  document.querySelector('[data-download-png]')?.addEventListener('click', async (e) => {
    const button = e.currentTarget;
    const svg = document.querySelector('.chart-wheel');
    if (!svg) return;

    const original = button.textContent;
    button.disabled = true;
    button.textContent = 'Rendering…';

    try {
      await exportPng(svg);
    } catch (err) {
      button.textContent = 'Could not render the image';
      setTimeout(() => { button.textContent = original; button.disabled = false; }, 2500);
      return;
    }
    button.textContent = original;
    button.disabled = false;
  });

  function exportPng(svg) {
    return new Promise((resolve, reject) => {
      const box = svg.viewBox.baseVal;
      const scale = 2; // enough to stay sharp on a phone screen and in print

      // An <img> will not lay out a percentage-width SVG, so the clone gets
      // concrete pixel dimensions.
      const clone = svg.cloneNode(true);
      clone.setAttribute('width', box.width);
      clone.setAttribute('height', box.height);

      const source = new XMLSerializer().serializeToString(clone);
      const url = URL.createObjectURL(new Blob([source], { type: 'image/svg+xml;charset=utf-8' }));

      const img = new Image();
      img.onload = () => {
        const canvas = document.createElement('canvas');
        canvas.width = box.width * scale;
        canvas.height = box.height * scale;

        const ctx = canvas.getContext('2d');
        ctx.fillStyle = '#fcfcfb';
        ctx.fillRect(0, 0, canvas.width, canvas.height);
        ctx.drawImage(img, 0, 0, canvas.width, canvas.height);
        URL.revokeObjectURL(url);

        canvas.toBlob((blob) => {
          if (!blob) { reject(new Error('canvas produced no image')); return; }
          const link = document.createElement('a');
          link.href = URL.createObjectURL(blob);
          link.download = 'birth-chart.png';
          link.click();
          setTimeout(() => URL.revokeObjectURL(link.href), 1000);
          resolve();
        }, 'image/png');
      };
      img.onerror = () => { URL.revokeObjectURL(url); reject(new Error('the svg could not be loaded')); };
      img.src = url;
    });
  }

  // ----------------------------------------------------------- PDF export

  // The PDF is the browser's own print output, styled by the @media print block
  // in style.css. Rendering the page that way keeps the wheel vector, the text
  // selectable and the glyphs in whatever font drew them on screen, none of
  // which survives rasterising the page to an image.
  //
  // A collapsed <details> prints as its summary alone, so the notes about where
  // this chart sits on a boundary would be missing from the file. Expanding them
  // is hung on beforeprint rather than done in the click handler: window.print()
  // returns at a different moment in each browser — some before the page has
  // been laid out for paper — so collapsing them again afterwards is only safe
  // once the browser says it has finished. It also means Ctrl+P produces exactly
  // what the button produces.
  let reopened = [];

  window.addEventListener('beforeprint', () => {
    reopened = [...document.querySelectorAll('main details:not([open])')];
    reopened.forEach((d) => { d.open = true; });
  });

  window.addEventListener('afterprint', () => {
    reopened.forEach((d) => { d.open = false; });
    reopened = [];
  });

  document.querySelector('[data-download-pdf]')?.addEventListener('click', () => {
    window.print();
  });
})();
