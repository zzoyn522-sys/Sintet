const reducedMotion = matchMedia('(prefers-reduced-motion: reduce)');

function colorChannels(value) {
  const match = /^#([0-9a-f]{6})$/i.exec(value || '');
  if (!match) return [220, 232, 255];
  const hex = match[1];
  return [0, 2, 4].map(offset => parseInt(hex.slice(offset, offset + 2), 16));
}

function randomPoint() {
  const lifetime = 12 + Math.random() * 12;
  return {x:Math.random(), y:Math.random(), depth:Math.random(), age:Math.random() * lifetime, lifetime};
}

function wrapPosition(position) {
  const span = 1.2;
  return ((position + .1) % span + span) % span - .1;
}

const reflections = [
  {position:.08, size:.12, weight:.50},
  {position:.17, size:.24, weight:.84},
  {position:.27, size:.09, weight:.60},
  {position:.48, size:.055, weight:.44},
  {position:.86, size:.36, weight:1},
  {position:.58, size:.12, weight:.43},
  {position:.72, size:.075, weight:.60},
  {position:.96, size:.14, weight:.40},
];

export function configureEffects(canvas, next) {
  if (!canvas) return;
  let config = {...next};
  const context = canvas.getContext('2d', {alpha:true});
  if (!context) return;
  let width = 0, height = 0, ratio = 1, frame = 0, lastTime = 0, elapsed = 0;
  const particles = [];

  function resize() {
    const rect = canvas.getBoundingClientRect();
    const nextWidth = Math.max(0, rect.width);
    const nextHeight = Math.max(0, rect.height);
    ratio = Math.min(2, window.devicePixelRatio || 1);
    if (nextWidth === width && nextHeight === height && canvas.width === Math.round(width * ratio) && canvas.height === Math.round(height * ratio)) return;
    width = nextWidth;
    height = nextHeight;
    canvas.width = Math.round(width * ratio);
    canvas.height = Math.round(height * ratio);
    context.setTransform(ratio, 0, 0, ratio, 0, 0);
  }

  function resizeList(list, count) {
    while (list.length < count) list.push(randomPoint());
    if (list.length > count) list.length = count;
  }

  function enabled() {
    return (config.glintsEnabled && config.glintCount > 0 && config.glintStrength > 0) ||
      (config.particlesEnabled && config.particleCount > 0 && config.particleOpacity > 0);
  }

  function drawGlints(rgb) {
    if (!config.glintsEnabled || !config.glintStrength) return;
    const count = Math.min(reflections.length, config.glintCount);
    const phase = elapsed * config.glintSpeed / 100 * config.speed / 50 * .24;
    const driftX = Math.sin(phase) * .045;
    const driftY = Math.sin(phase * .7) * .03;
    for (let index = 0; index < count; index++) {
      const reflection = reflections[count === 1 ? 4 : index];
      const t = reflection.position;
      const x = width * (.06 + t * .87 + driftX * (1 - t * .45));
      const y = height * (.41 - t * .16 + driftY);
      const size = config.glintSize * reflection.size;
      const alpha = config.glintStrength / 100 * reflection.weight * (.20 + .03 * Math.sin(phase + t));
      const haloSize = size * 1.8;
      const halo = context.createRadialGradient(x, y, 0, x, y, haloSize);
      halo.addColorStop(0, `rgba(${rgb},${alpha * .38})`);
      halo.addColorStop(.48, `rgba(${rgb},${alpha * .22})`);
      halo.addColorStop(1, `rgba(${rgb},0)`);
      context.fillStyle = halo;
      context.fillRect(x - haloSize, y - haloSize, haloSize * 2, haloSize * 2);

      const glass = context.createRadialGradient(x - size * .27, y - size * .30, 0, x, y, size);
      glass.addColorStop(0, `rgba(${rgb},${alpha * .85})`);
      glass.addColorStop(.65, `rgba(${rgb},${alpha * .31})`);
      glass.addColorStop(1, `rgba(${rgb},${alpha * .14})`);
      context.fillStyle = glass;
      context.beginPath();
      for (let corner = 0; corner < 6; corner++) {
        const angle = Math.PI / 6 + corner * Math.PI / 3 + t * .4;
        const px = x + Math.cos(angle) * size;
        const py = y + Math.sin(angle) * size;
        if (corner === 0) context.moveTo(px, py);
        else context.lineTo(px, py);
      }
      context.closePath();
      context.fill();
      const rim = context.createLinearGradient(x - size, y - size, x + size, y + size);
      rim.addColorStop(0, `rgba(${rgb},${alpha})`);
      rim.addColorStop(.65, `rgba(${rgb},${alpha * .12})`);
      rim.addColorStop(1, `rgba(${rgb},${alpha * .58})`);
      context.strokeStyle = rim;
      context.lineWidth = Math.min(1.5, Math.max(.5, size * .045));
      context.stroke();
    }
  }

  function drawParticle(point, delta, rgb) {
    const depth = config.particleDepth / 100;
    const near = point.depth * depth;
    const minSize = config.particleMinSize;
    const maxSize = Math.max(minSize, config.particleMaxSize);
    const radius = (minSize + (maxSize - minSize) * near ** 2) / 2;
    const movement = delta * config.speed / 50 * (.6 + near * .65);
    point.x = wrapPosition(point.x + config.particleSpeedX * .48 * movement / width);
    point.y = wrapPosition(point.y + config.particleSpeedY * .48 * movement / height);
    point.age = (point.age + delta * config.speed / 50) % point.lifetime;
    const life = Math.sin(Math.PI * point.age / point.lifetime) ** 2;
    const fade = config.particleFade / 100;
    const opacity = config.particleOpacity / 100 * (.30 + near * .35) * (1 - fade * (1 - life));
    if (opacity < .004) return;
    const x = point.x * width;
    const y = point.y * height;
    context.fillStyle = `rgba(${rgb},${opacity})`;
    context.beginPath();
    context.arc(x, y, radius, 0, Math.PI * 2);
    context.fill();
  }

  function drawFrame(delta = 0) {
    if (!width || !height) return;
    context.clearRect(0, 0, width, height);
    drawGlints(colorChannels(config.glintColor).join(','));
    resizeList(particles, config.particlesEnabled ? Math.min(180, Math.max(0, Number(config.particleCount || 0))) : 0);
    const particleRGB = colorChannels(config.particleColor).join(',');
    for (const point of particles) drawParticle(point, delta, particleRGB);
  }

  function stop() {
    if (frame) cancelAnimationFrame(frame);
    frame = 0;
    lastTime = 0;
  }

  function tick(time) {
    frame = 0;
    if (!config.animate || reducedMotion.matches || document.hidden || !enabled()) return;
    const delta = lastTime ? Math.max(0, Math.min(48, time - lastTime)) / 1000 : 0;
    lastTime = time;
    elapsed += delta;
    drawFrame(delta);
    frame = requestAnimationFrame(tick);
  }

  function update(nextConfig) {
    config = {...config, ...nextConfig};
    resize();
    if (!enabled()) {
      stop();
      context.clearRect(0, 0, width, height);
      return;
    }
    drawFrame();
    if (config.animate && !reducedMotion.matches && !document.hidden && !frame) frame = requestAnimationFrame(tick);
    else if (!config.animate || reducedMotion.matches) stop();
  }

  const onVisibility = () => {
    if (document.hidden) stop();
    else update(config);
  };
  const onReducedMotion = () => update(config);
  window.addEventListener('resize', () => update(config), {passive:true});
  document.addEventListener('visibilitychange', onVisibility);
  reducedMotion.addEventListener?.('change', onReducedMotion);
  update(config);
  return update;
}
