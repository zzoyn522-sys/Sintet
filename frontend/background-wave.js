const surfaces = [...document.querySelectorAll('.background-preview-wave canvas, .appearance-wave-preview')]
  .map(canvas => ({ canvas, context: canvas.getContext('2d'), visible: false, image: null, preview: canvas.classList.contains('appearance-wave-preview') }));
const reducedMotion = matchMedia('(prefers-reduced-motion: reduce)');
const waveWidth = 1100;
const waveHeight = 120;
const maxOffset = Number(document.querySelector('input[name="waveOffset"]').max);
const quarterTurn = Math.PI / 2;
const points = [
  { x: -32, y: 46, slope: -.37, amplitude: 0, offset: 0 },
  { x: 140, y: 22, slope: 0, amplitude: 7, offset: 0 },
  { x: 350, y: 48, slope: 0, amplitude: 10, offset: quarterTurn },
  { x: 595, y: 18, slope: 0, amplitude: 7, offset: 2 * quarterTurn },
  { x: 825, y: 45, slope: 0, amplitude: 10, offset: 3 * quarterTurn },
  { x: 1132, y: 21, slope: -.18, amplitude: 0, offset: 0 },
];

let frame;
let previousTime;
let elapsed = 0;
let period = 12000;
let config = {enabled: false};
let color;

function resizeSurfaces() {
  const density = window.devicePixelRatio || 1;
  for (const surface of surfaces) {
    const { canvas, context } = surface;
    const { width, height } = canvas.getBoundingClientRect();
    const pixelWidth = Math.round(width * density);
    const pixelHeight = Math.round(height * density);
    surface.visible = Boolean(pixelWidth && pixelHeight);
    if (!surface.visible || (surface.image?.width === pixelWidth && surface.image.height === pixelHeight)) continue;

    canvas.width = pixelWidth;
    canvas.height = pixelHeight;
    surface.image = context.createImageData(pixelWidth, pixelHeight);
  }
  drawWave(elapsed);
}

function heightAt(x, heights) {
  const endIndex = points.findIndex(point => point.x >= x);
  const start = points[endIndex - 1];
  const end = points[endIndex];
  const span = end.x - start.x;
  const t = (x - start.x) / span;
  const blend = t * t * (3 - 2 * t);
  return heights[endIndex - 1] * (1 - blend) + heights[endIndex] * blend
    + span * t * (1 - t) * ((1 - t) * start.slope - t * end.slope);
}

function slopeAt(x, heights) {
  const endIndex = points.findIndex(point => point.x >= x);
  const start = points[endIndex - 1];
  const end = points[endIndex];
  const span = end.x - start.x;
  const t = (x - start.x) / span;
  const averageSlope = (heights[endIndex] - heights[endIndex - 1]) / span;
  return 6 * t * (1 - t) * averageSlope
    + (1 - t) * (1 - 3 * t) * start.slope
    + t * (3 * t - 2) * end.slope;
}

function renderSurface({ context, image, preview }, heights) {
  const { width, height, data } = image;
  const step = waveWidth / width;
  const scaleY = height / (waveHeight + (preview ? maxOffset : 0));
  const offsetY = preview ? config.offset * scaleY : 0;
  const bottomY = offsetY + waveHeight * scaleY;
  const rowStride = width * 4;
  const sampleRadius = 26;
  const trailStrength = config.trail / 100;
  const trailDepth = trailStrength > 0 ? (22 + 64 * trailStrength) * scaleY : 0;
  const [red, green, blue] = color;
  data.fill(0);

  for (let column = 0; column < width; column++) {
    const x = (column + .5) * step;
    const edgeY = offsetY + heightAt(x, heights) * scaleY;
    const slope = slopeAt(x, heights);
    const bend = slopeAt(x + sampleRadius, heights) - slopeAt(x - sampleRadius, heights);
    const normal = Math.sqrt(1 + slope * slope);
    const curvature = bend / (2 * sampleRadius * normal * normal * normal);
    const bendIntensity = curvature / .0023;
    const strength = 1 - Math.exp(-bendIntensity * bendIntensity);
    const reflection = curvature >= 0 ? strength : strength * .55;
    const edge = Math.min(1, x / 150, (waveWidth - x) / 150);
    const fade = edge * edge * (3 - 2 * edge);
    const brightness = 255 * Math.min(1, (.065 + .91 * reflection) * config.intensity / 100) * fade;
    if (brightness < .5) continue;

    const rimDepth = 1.35 * scaleY * normal;
    const depth = Math.min(trailDepth * normal, bottomY - edgeY);
    const glowStrength = .63 * trailStrength;
    // Stop once even a full pixel rounds down to transparent.
    const visibleDepth = glowStrength ? depth * (1 - Math.cbrt(.5 / (brightness * glowStrength))) : 0;
    const firstRow = Math.max(0, Math.floor(edgeY));
    const lastRow = Math.min(height, Math.ceil(edgeY + Math.max(rimDepth, visibleDepth + .5)));

    for (let row = firstRow, index = firstRow * rowStride + column * 4; row < lastRow; row++, index += rowStride) {
      // Integrate the rim over each pixel so the thin edge stays sharp as it moves.
      const start = Math.max(row, edgeY);
      const rim = Math.max(0, Math.min(row + 1, edgeY + rimDepth) - start);
      const offset = (start + row + 1) / 2 - edgeY;
      const falloff = depth > 0 ? Math.max(0, 1 - offset / depth) : 0;
      const glow = (row + 1 - start) * glowStrength * falloff * falloff * falloff;
      const alpha = brightness * (rim + (1 - rim) * glow);
      if (alpha < .5) continue;
      data[index] = red;
      data[index + 1] = green;
      data[index + 2] = blue;
      data[index + 3] = alpha;
    }
  }

  context.putImageData(image, 0, 0);
}

function drawWave(time) {
  const phase = 2 * Math.PI * (time % period) / period;
  const heights = points.map(point => point.y + point.amplitude * (config.amplitude / 100) * Math.sin(phase + point.offset));
  for (const surface of surfaces) {
    if (!surface.visible) continue;
    if (config.enabled && config.intensity > 0) renderSurface(surface, heights);
    else surface.context.clearRect(0, 0, surface.image.width, surface.image.height);
  }
}

function animateWave(time) {
  if (previousTime !== undefined) elapsed += Math.min(time - previousTime, 50);
  previousTime = time;
  drawWave(elapsed);
  frame = requestAnimationFrame(animateWave);
}

function shouldAnimate() {
  return config.enabled && config.animate && config.intensity > 0 && config.amplitude > 0 && !document.hidden && !reducedMotion.matches;
}

function updateMotion() {
  cancelAnimationFrame(frame);
  previousTime = undefined;
  if (shouldAnimate()) frame = requestAnimationFrame(animateWave);
}

export function configureWave(settings) {
  const wasMoving = shouldAnimate();
  config = settings;
  document.documentElement.style.setProperty('--wave-offset', `${settings.offset}px`);
  color = [1, 3, 5].map(index => Number.parseInt(settings.color.slice(index, index + 2), 16));
  const newPeriod = 18000 - 120 * settings.speed;
  if (newPeriod !== period) {
    elapsed = (elapsed % period) / period * newPeriod;
    period = newPeriod;
  }
  resizeSurfaces();
  if (wasMoving !== shouldAnimate()) updateMotion();
}

reducedMotion.addEventListener('change', updateMotion);
document.addEventListener('visibilitychange', updateMotion);
const observer = new ResizeObserver(resizeSurfaces);
for (const { canvas } of surfaces) observer.observe(canvas, { box: 'device-pixel-content-box' });
window.addEventListener('resize', resizeSurfaces);

if (document.documentElement.dataset.backgroundExperiment === 'glow') {
  configureWave({enabled: true, animate: true, color: '#ffffff', intensity: 100, amplitude: 100, trail: 50, speed: 50, offset: 0});
}
