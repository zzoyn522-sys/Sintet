# Shared UI: dropdowns

`select.js` enhances every single-value `<select>` automatically. Styling and design tokens live in `select.css`; both are loaded once by index.html after app.js. Dynamically added selects are mounted automatically. Multi-selects are left native.

Use ordinary HTML:

<select id="loader" aria-label="Загрузчик">
  <option value="vanilla">Vanilla</option>
  <option value="fabric">Fabric</option>
</select>

Keep application state on the native select. Read `.value` and listen to `input` / `change` as usual. To update the value from code:

const select = document.querySelector('#loader');
select.value = 'fabric';
select.dispatchEvent(new Event('change', { bubbles: true }));

For a silent visual refresh, call `SintetUI.select.refresh(select)`. Changes to options, labels and disabled state are observed automatically. Existing label elements focus the custom trigger. Removing the source/containing UI disposes the popup.

The shared component owns popup placement, single-open behavior, keyboard navigation, disabled options, type-to-search, Escape cancellation, outside click and focus dismissal. Menus are mounted on document.body or their containing top-layer dialog/popover to avoid clipping and modal inertness. Placement compensates transformed/acrylic ancestors and follows scrolled anchors. Use CSS variables prefixed `--select-` for app-wide theme changes instead of per-screen styles.

Verification: browser checks cover mouse and keyboard selection, cancellation, outside click, Tab, persistence, dynamic controls and options, disabled options, upward placement, cleanup and runtime errors.
