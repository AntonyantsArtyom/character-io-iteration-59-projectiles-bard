import * as THREE from './assets/three/three.module.min.js';
import { VOXLoader, VOXMesh } from './assets/three/VOXLoader.js';
import { createCourtyard, clearCourtyard } from './map-art.js';

import { createLocalProjectiles, muzzleOffset, projectileSpec } from './local-projectiles.js';

const heroes = ['knight', 'archer', 'mage', 'rogue', 'berserker', 'bard'];
const teamColors = { blue: '#65baff', red: '#ff737d' };
const meleeTrails = {
  knight: { radius: 1.64, arc: 2.05, width: 0.27, height: 0.78, tilt: 0.25, color: '#ffd18a', duration: 175 },
  rogue: { radius: 1.29, arc: 1.32, width: 0.13, height: 0.69, tilt: -0.18, color: '#9cf8ed', duration: 125 },
  berserker: { radius: 1.72, arc: 2.58, width: 0.42, height: 0.85, tilt: 0.38, color: '#ff9a65', duration: 205 },
};

function slashGeometry(style, edge) {
  const positions = [];
  const faces = [];
  const segments = 16;
  for (let i = 0; i <= segments; i++) {
    const u = i / segments;
    const angle = (u - 0.5) * style.arc;
    const taper = Math.pow(Math.sin(Math.PI * u), 0.9) * (0.52 + 0.48 * u);
    const y = style.height + style.tilt * (u - 0.5) + 0.09 * Math.sin(Math.PI * u);
    const inner = style.radius - style.width * taper * (edge ? 0.17 : 1);
    positions.push(inner * Math.cos(angle), y - style.width * taper * (edge ? 0.09 : 0.5), inner * Math.sin(angle));
    positions.push(style.radius * Math.cos(angle), y, style.radius * Math.sin(angle));
    if (i > 0) {
      const n = 2 * i;
      faces.push(n - 2, n - 1, n, n - 1, n + 1, n);
    }
  }
  const geometry = new THREE.BufferGeometry();
  geometry.setAttribute('position', new THREE.Float32BufferAttribute(positions, 3));
  geometry.setIndex(faces);
  return geometry;
}

const windupSectors = new Map(Object.entries({
  knight: { reach: 1.8, sectorDot: 0.5 },
  rogue: { reach: 1.45, sectorDot: 0.79 },
  berserker: { reach: 1.9, sectorDot: 0.25 },
}).map(([hero, { reach, sectorDot }]) => {
  const halfAngle = Math.acos(sectorDot);
  return [hero, new THREE.RingGeometry(reach - 0.14, reach, 24, 1, -halfAngle, 2 * halfAngle)
    .rotateX(-Math.PI / 2)];
}));

const poseFiles = { walkA: 'walk-a', walkB: 'walk-b', windup: 'windup', strike: 'strike' };

export async function loadModels() {
  const loader = new VOXLoader();
  async function loadPose(path) {
    const chunks = await loader.loadAsync(new URL(path, import.meta.url).href);
    if (!chunks?.length) throw new Error('Пустая модель ' + path);
    return new VOXMesh(chunks[0]);
  }
  const models = await Promise.all(heroes.map(async hero => {
    const [idle, ...frames] = await Promise.all([
      loadPose('./models/' + hero + '.vox'),
      ...Object.values(poseFiles).map(pose => loadPose('./models/frames/' + hero + '-' + pose + '.vox')),
    ]);
    idle.geometry.computeBoundingBox();
    const feet = idle.geometry.boundingBox.min.y;
    for (const mesh of [idle, ...frames]) {
      mesh.geometry.translate(0, -feet, -0.5);
      mesh.geometry.scale(0.095, 0.095, 0.095);
    }
    const poses = { idle: idle.geometry };
    Object.keys(poseFiles).forEach((pose, i) => {
      poses[pose] = frames[i].geometry;
      frames[i].material.dispose();
    });
    return [hero, { poses, material: idle.material }];
  }));
  const projectiles = await Promise.all([
    ['arrow', 0.057],
    ['magic-bolt', 0.076],
    ['meteor', 0.09],
  ].map(async ([name, scale]) => {
    const mesh = await loadPose('./models/projectiles/' + name + '.vox');
    mesh.geometry.center();
    mesh.geometry.scale(scale, scale, scale);
    if (name === 'magic-bolt') {
      mesh.material.dispose();
      mesh.material = new THREE.MeshBasicMaterial({ vertexColors: true, toneMapped: false });
    } else if (name === 'meteor') {
      mesh.material.emissive.set('#bc201d');
      mesh.material.emissiveIntensity = 0.15;
    }
    return [name, { geometry: mesh.geometry, material: mesh.material }];
  }));
  return { heroes: new Map(models), projectiles: new Map(projectiles) };
}

export function createScene(canvas, labels) {
  const renderer = new THREE.WebGLRenderer({ canvas, antialias: true });
  renderer.setClearColor('#121822');
  const scene = new THREE.Scene();
  const camera = new THREE.OrthographicCamera(-13, 13, 13, -13, 0.1, 200);
  const ray = new THREE.Raycaster();
  const ground = new THREE.Plane(new THREE.Vector3(0, 1, 0));
  const cursor = new THREE.Vector2();
  const point = new THREE.Vector3();
  const projected = new THREE.Vector3();
  scene.add(new THREE.HemisphereLight('#e6efff', '#777067', 1.8));
  const sun = new THREE.DirectionalLight('#fff0d9', 2.25);
  sun.position.set(-8, 16, 7);
  scene.add(sun);

  const world = new THREE.Group();
  const actors = new THREE.Group();
  const effects = new THREE.Group();
  scene.add(world, actors, effects);
  const dashStreak = new THREE.BoxGeometry(1, 1, 1);
  const disk = new THREE.CircleGeometry(1, 48).rotateX(-Math.PI / 2);
  const ring = new THREE.RingGeometry(0.83, 1, 48).rotateX(-Math.PI / 2);
  const soundWave = new THREE.RingGeometry(0.15, 0.23, 16, 1, -1.1, 2.2).rotateX(-Math.PI / 2);
  const white = new THREE.MeshBasicMaterial({ color: '#ffffff' });
  const look = { x: 0, z: 0 };
  const cameraRise = 32;
  const cameraBack = 8.5;
  const groundProjection = cameraRise / Math.hypot(cameraRise, cameraBack);
  const transparent = new THREE.MeshStandardMaterial({
    vertexColors: true, transparent: true, opacity: 0.42, depthWrite: false,
  });
  const basic = new Map();
  const slashMeshes = new Map();
  const players = new Map();
  const transient = new Map();
  let templates = new Map();
  let projectileModels = new Map();
  let map = null;
  let width = 0, height = 0, pixelRatio = 0;
  let lastTime = 0;
  let lastSnapshotTick = -1;
  let snapshotAt = 0;
  let needsFit = true;
  const frameSeen = new Set();
  const localProjectiles = createLocalProjectiles();

  function material(color, opacity = 1) {
    const key = color + '/' + opacity;
    if (!basic.has(key)) {
      basic.set(key, new THREE.MeshBasicMaterial({
        color, opacity, transparent: opacity < 1, depthWrite: opacity === 1,
      }));
    }
    return basic.get(key);
  }

  function removePlayer(id, actor) {
    actors.remove(actor.root);
    actor.label.remove();
    players.delete(id);
  }

  function resetRound() {
    lastSnapshotTick = -1;
    localProjectiles.reset();
    needsFit = true;
    effects.clear();
    transient.clear();
    for (const [id, actor] of players) removePlayer(id, actor);
  }

  function setMap(next) {
    map = next;
    resetRound();
    clearCourtyard(world);
    if (!map) return;
    look.x = (map.bounds.min.x + map.bounds.max.x) / 2;
    look.z = (map.bounds.min.z + map.bounds.max.z) / 2;
    world.add(createCourtyard(map));
  }

  function resize() {
    const w = Math.max(1, canvas.clientWidth);
    const h = Math.max(1, canvas.clientHeight);
    const ratio = Math.min(window.devicePixelRatio || 1, 2);
    if (w !== width || h !== height || ratio !== pixelRatio) {
      width = w;
      height = h;
      pixelRatio = ratio;
      renderer.setPixelRatio(ratio);
      renderer.setSize(w, h, false);
      needsFit = true;
    }
    if (!needsFit) return;
    needsFit = false;
    const { min, max } = map?.bounds ?? { min: {x:-12,z:-12}, max: {x:12,z:12} };
    const x = (min.x + max.x) / 2;
    const z = (min.z + max.z) / 2;
    const aspect = width / height;
    const halfH = Math.min(7.2, (max.x - min.x) / (2 * aspect), (max.z - min.z) * groundProjection / 2);
    camera.left = -halfH * aspect;
    camera.right = halfH * aspect;
    camera.top = halfH;
    camera.bottom = -halfH;
    camera.updateProjectionMatrix();
    follow(map ? look : { x, z });
  }

  function follow(target) {
    if (!target) return;
    look.x = target.x;
    look.z = target.z;
    let { x, z } = target;
    if (map) {
      const { min, max } = map.bounds;
      const halfX = Math.min(camera.right, (max.x - min.x) / 2);
      const halfZ = Math.min(camera.top / groundProjection, (max.z - min.z) / 2);
      x = THREE.MathUtils.clamp(x, min.x + halfX, max.x - halfX);
      z = THREE.MathUtils.clamp(z, min.z + halfZ, max.z - halfZ);
    }
    camera.position.set(x, cameraRise, z + cameraBack);
    camera.lookAt(x, 0, z);
    camera.updateMatrixWorld();
  }

  function groundPoint(clientX, clientY) {
    resize();
    const rect = canvas.getBoundingClientRect();
    cursor.set((clientX - rect.left - canvas.clientLeft) / width * 2 - 1,
      1 - (clientY - rect.top - canvas.clientTop) / height * 2);
    ray.setFromCamera(cursor, camera);
    const hit = ray.ray.intersectPlane(ground, point);
    return hit ? { x: hit.x, z: hit.z } : null;
  }

  function playerActor(p) {
    const root = new THREE.Group();
    const template = templates.get(p.hero);
    const model = new THREE.Mesh(template.poses.idle, template.material);
    root.add(model);
    const markers = new THREE.Group();
    root.add(markers);
    const marker = new THREE.Mesh(ring, material(teamColors[p.team] ?? '#c0b8ae'));
    marker.position.y = 0.035;
    marker.scale.setScalar(0.52);
    markers.add(marker);
    if (p.id === map.playerId) {
      const own = new THREE.Mesh(ring, white);
      own.position.y = 0.04;
      own.scale.setScalar(0.63);
      markers.add(own);
    }
    const label = document.createElement('div');
    label.className = 'unit-label';
    const name = document.createElement('span');
    name.textContent = p.nick;
    name.style.color = teamColors[p.team] ?? '#fff';
    const bar = document.createElement('div');
    const hp = document.createElement('i');
    bar.append(hp);
    label.append(name);
    if (p.bot) {
      const icon = document.createElement('span');
      icon.className = 'bot-icon';
      icon.textContent = '🎮';
      icon.title = 'Бот';
      label.append(icon);
    }
    label.append(bar);
    labels.append(label);
    actors.add(root);
    return { root, model, markers, label, hp, template, stride: 0, lastMoved: -Infinity, fellAt: null, dash: null };
  }

  function effect(key, geometry, mat) {
    frameSeen.add(key);
    let mesh = transient.get(key);
    if (!mesh) {
      mesh = new THREE.Mesh(geometry, mat);
      effects.add(mesh);
      transient.set(key, mesh);
    }
    mesh.material = mat;
    return mesh;
  }

  function showArrowFan(player, pos, attack, tick) {
    const age = (tick - attack.hitTick) / 5;
    if (age < 0 || age >= 1) return;
    const arrow = projectileModels.get('arrow');
    const distance = 0.35 + 3.45 * age;
    for (let i = 0; i < 7; i++) {
      const angle = attack.aim + (i - 3) * Math.PI / 12;
      const fx = effect('fan:' + player.id + ':' + attack.id + ':' + i, arrow.geometry, arrow.material);
      fx.userData.originX ??= pos.x;
      fx.userData.originZ ??= pos.z;
      fx.position.set(fx.userData.originX + Math.cos(angle) * distance,
        0.72 + 0.09 * Math.sin(Math.PI * age),
        fx.userData.originZ + Math.sin(angle) * distance);
      fx.rotation.y = Math.PI / 2 - angle;
      fx.scale.setScalar(0.68 * Math.min(1, (1 - age) * 5));
    }
  }

  function slashMaterial(color, opacity) {
    const key = 'slash/' + color + '/' + opacity;
    if (!basic.has(key)) {
      basic.set(key, new THREE.MeshBasicMaterial({
        color, opacity, side: THREE.DoubleSide, transparent: true, depthWrite: false,
        blending: THREE.AdditiveBlending, toneMapped: false,
      }));
    }
    return basic.get(key);
  }

  function showSlash(key, pos, style, aim, time, age) {
    if (!slashMeshes.has(style)) {
      slashMeshes.set(style, [slashGeometry(style, false), slashGeometry(style, true)]);
    }
    const meshes = slashMeshes.get(style);
    for (let i = 0; i < meshes.length; i++) {
      const id = key + ':' + i;
      const start = transient.get(id)?.userData.startTime ?? time;
      const progress = Math.min(1, Math.max(age * 0.4, (time - start) / style.duration));
      const fade = Math.round((1 - progress) * 8) / 8;
      const fx = effect(id, meshes[i], slashMaterial(i ? '#fff5dc' : style.color, fade * (i ? 0.9 : 0.55)));
      fx.userData.startTime = start;
      fx.position.set(pos.x, i * 0.008, pos.z);
      fx.rotation.y = -aim + (0.45 - progress * 0.7);
      fx.scale.set(0.84 + progress * 0.21, 1, 0.84 + progress * 0.21);
    }
  }

  function showDeath(actor, player, time) {
    actor.fellAt ??= time;
    const elapsed = time - actor.fellAt;
    actor.root.visible = elapsed < 560;
    actor.label.hidden = true;
    actor.markers.visible = false;
    actor.root.position.set(player.x, 0, player.z);
    const fall = Math.min(1, elapsed / 360);
    const tilt = Math.PI / 2 * fall * fall;
    actor.model.geometry = actor.template.poses.idle;
    actor.model.material = actor.template.material;
    actor.model.rotation.set(tilt, Math.PI / 2 - player.aim, 0, 'YXZ');
    actor.model.position.y = 0.36 * Math.sin(tilt);
  }

  function dashFrame(actor, player, display, time, tick) {
    const attack = player.secondaryAttack;
    const path = attack?.dash;
    if (!actor || !player.hp || !path) return null;
    if (actor.dash?.id !== attack.id) {
      if (tick - attack.startTick > 8) return null;
      actor.dash = { id: attack.id, at: time };
    }
    const elapsed = time - actor.dash.at;
    const distance = Math.hypot(path.to.x - path.from.x, path.to.z - path.from.z);
    const duration = THREE.MathUtils.clamp(distance * 40, 65, 175);
    if (elapsed >= duration + 65) return null;
    const phase = Math.min(1, elapsed / duration);
    const progress = 1 - (1 - phase) ** 2;
    const catchup = Math.max(0, (elapsed - duration) / 65);
    return {
      x: THREE.MathUtils.lerp(THREE.MathUtils.lerp(path.from.x, path.to.x, progress), display.x, catchup),
      z: THREE.MathUtils.lerp(THREE.MathUtils.lerp(path.from.z, path.to.z, progress), display.z, catchup),
      aim: attack.aim, phase, progress, distance, path, catchup,
    };
  }

  function showDashTrail(id, dash) {
    const length = dash.distance * Math.min(dash.progress, 0.46);
    if (length < 0.02) return;
    const { from, to } = dash.path;
    const dx = to.x - from.x, dz = to.z - from.z;
    const middle = dash.progress - length / (2 * dash.distance);
    for (const side of [-1, 1]) {
      const trail = effect('dash:' + id + ':' + side, dashStreak, material('#ffd79a', side === 1 ? 0.38 : 0.58));
      trail.position.set(from.x + dx * middle - Math.sin(dash.aim) * side * 0.26,
        0.12 + side * 0.025, from.z + dz * middle + Math.cos(dash.aim) * side * 0.26);
      trail.rotation.y = Math.PI / 2 - dash.aim;
      trail.scale.set(0.075 * (1 - dash.catchup), 0.055, length * (1 - dash.catchup));
    }
  }

  function drawControlZones(states) {
    for (const [i, zone] of (map.zones ?? []).entries()) {
      const state = states?.[i];
      const color = teamColors[state?.owner] ?? '#c8b98b';
      const fill = effect('control-fill:' + i, disk, material(color, 0.18));
      fill.position.set(zone.x, 0.044, zone.z);
      fill.scale.setScalar(zone.radius);
      const edge = effect('control-edge:' + i, ring, material(state?.contested ? '#ffcd65' : color, 0.74));
      edge.position.set(zone.x, 0.047, zone.z);
      edge.scale.setScalar(zone.radius);
      if (state?.progress > 0) {
        const meter = effect('control-meter:' + i, disk, material(teamColors[state.capturing] ?? color, 0.34));
        meter.position.set(zone.x, 0.050, zone.z);
        meter.scale.setScalar(zone.radius * Math.sqrt(state.progress));
      }
    }
  }

  function render(snapshot, recentMeteors, time, predicted, samples, feedback, primaryAttempt) {
    resize();
    const dt = Math.min(0.1, (time - (lastTime || time)) / 1000);
    lastTime = time;
    frameSeen.clear();
    const seenPlayers = new Set();
    let localHit = false;
    if (map && snapshot) {
      drawControlZones(snapshot.zones);
      const tick = snapshot.serverTick;
      const local = snapshot.players.find(p => p.id === map.playerId);
      const localDash = local && dashFrame(players.get(local.id), local, predicted ?? local, time, tick);
      follow(localDash ?? predicted ?? local);
      if (tick !== lastSnapshotTick) {
        lastSnapshotTick = tick;
        snapshotAt = time;
      }
      const visualTick = tick + Math.min(2, (time - snapshotAt) * map.tickRate / 1000);
      for (const p of snapshot.players) {
        seenPlayers.add(p.id);
        let actor = players.get(p.id);
        if (!actor) {
          actor = playerActor(p);
          actor.root.position.set(p.x, 0, p.z);
          players.set(p.id, actor);
        }
        if (p.hp === 0) {
          showDeath(actor, p, time);
          continue;
        }
        if (actor.fellAt !== null) {
          actor.fellAt = null;
          actor.root.visible = true;
          actor.label.hidden = false;
          actor.markers.visible = true;
          actor.root.position.set(p.x, 0, p.z);
          actor.model.rotation.x = 0;
          actor.model.position.y = 0;
          actor.lastMoved = -Infinity;
          actor.stride = 0;
        }
        const ownPrediction = p.id === map.playerId && predicted;
        const display = ownPrediction || (p.id !== map.playerId && samples?.player(p)) || p;
        const pos = actor.root.position;
        const dash = p.id === map.playerId
          ? localDash ?? dashFrame(actor, p, display, time, tick)
          : dashFrame(actor, p, display, time, tick);
        const target = dash ?? display;
        const distance = Math.hypot(target.x - pos.x, target.z - pos.z);
        const blend = dash || ownPrediction || samples || distance > 2 ? 1 : 1 - Math.exp(-24 * dt);
        pos.x += (target.x - pos.x) * blend;
        pos.z += (target.z - pos.z) * blend;
        const traveled = distance * blend;
        if (!dash && traveled > 0.002 && traveled < 2) {
          actor.stride += traveled;
          actor.lastMoved = time;
        }
        let pose = time - actor.lastMoved < 95 ? (Math.floor(actor.stride / 0.65) % 2 ? 'walkB' : 'walkA') : 'idle';
        actor.model.rotation.set(0, Math.PI / 2 - display.aim, 0, 'YXZ');
        actor.model.position.y = 0;
        actor.model.material = p.invisibleUntil > tick ? transparent : actor.template.material;
        actor.hp.style.width = 100 * p.hp / p.maxHp + '%';
        projected.set(pos.x, 1.6, pos.z).project(camera);
        actor.label.style.transform = 'translate(' + ((projected.x + 1) * width / 2) + 'px,' +
          ((1 - projected.y) * height / 2) + 'px) translate(-50%,-100%)';
        const animationTick = p.id === map.playerId ? tick : samples?.tick ?? tick;
        const windupSector = windupSectors.get(p.hero);
        // Show melee windup warnings on the live clock.
        const primaryTick = windupSector ? visualTick : animationTick;
        const primary = p.attack;
        const windingUp = primary && primaryTick < primary.hitTick;
        const primaryAim = windingUp && primary.windupMoveMultiplier && ownPrediction ? display.aim : primary?.aim;
        const secondary = p.secondaryAttack;
        const swing = meleeTrails[p.hero];
        if (primary && primaryTick >= primary.startTick && primaryTick < primary.hitTick + 5) {
          pose = primaryTick < primary.hitTick ? 'windup' : 'strike';
          actor.model.rotation.y = Math.PI / 2 - primaryAim;
        }
        if (windupSector && primary && primaryTick >= primary.startTick && primaryTick < primary.hitTick) {
          const warning = effect('windup:' + p.id, windupSector,
            material(p.team === map.team ? '#ffd18a' : '#ff795e', primaryTick >= primary.hitTick - 5 ? 0.92 : 0.56));
          warning.position.set(pos.x, 0.073, pos.z);
          warning.rotation.y = -primaryAim;
        }
        if (swing && primary && primaryTick >= primary.hitTick &&
            primaryTick < primary.hitTick + swing.duration * map.tickRate / 1000) {
          const age = (primaryTick - primary.hitTick) / (swing.duration * map.tickRate / 1000);
          showSlash('attack:' + p.id + ':' + primary.id, pos, swing, primary.aim, time, age);
        }
        if (p.id === map.playerId && feedback && time < feedback.until) {
          if (!swing || (feedback.secondary && p.hero === 'knight')) {
            const fx = effect('local-feedback', ring, material(feedback.secondary ? '#ffd08a' : '#ffffff', 0.72));
            fx.position.set(pos.x, 0.075, pos.z);
            fx.scale.setScalar(0.72);
          }
          if (!feedback.secondary && !swing && !projectileSpec(map, p.hero)) {
            const windup = (p.hero === 'archer' ? 1 : 2) * 1000 / map.tickRate;
            const age = (time - feedback.startedAt - windup) / 75;
            if (age >= 0 && age < 1) {
              const color = p.hero === 'mage' ? '#ff704a' : p.hero === 'bard' ? '#f5d783' : '#f7e8b3';
              const muzzle = effect('local-release', dashStreak, material(color, 0.84));
              if (muzzle.userData.actionId !== feedback.id) {
                muzzle.userData.actionId = feedback.id;
                muzzle.position.set(pos.x + Math.cos(feedback.aim) * muzzleOffset, 0.65,
                  pos.z + Math.sin(feedback.aim) * muzzleOffset);
              }
              muzzle.rotation.y = Math.PI / 2 - feedback.aim;
              muzzle.scale.set(0.09 * (1 - age), 0.09, 0.29 * (1 - age));
            }
          }
          const confirmed = feedback.secondary ? secondary : primary;
          if ((confirmed?.id ?? 0) < feedback.id && (!feedback.secondary || (p.hero !== 'rogue' && p.hero !== 'berserker'))) {
            pose = 'windup';
            actor.model.rotation.y = Math.PI / 2 - feedback.aim;
          }
        }
        if (secondary && animationTick >= secondary.startTick && animationTick < secondary.startTick + 6) {
          if (p.hero !== 'rogue' && p.hero !== 'berserker') {
            const releaseTick = p.hero === 'mage' ? secondary.startTick + 3 : secondary.hitTick;
            pose = animationTick < releaseTick ? 'windup' : 'strike';
            actor.model.rotation.y = Math.PI / 2 - secondary.aim;
          }
          if (p.hero === 'archer') showArrowFan(p, pos, secondary, p.id === map.playerId ? visualTick : animationTick);
        }
        if (p.inspirationCharges || p.inspirationSpeedUntil > visualTick) {
          const marker = effect('inspired:' + p.id, ring, material('#f5d783', 0.66));
          marker.position.set(pos.x, 0.075, pos.z);
          marker.scale.setScalar(0.62);
        }
        if (p.hero === 'bard' && secondary) {
          const age = (visualTick - secondary.startTick) / 12;
          if (age >= 0 && age < 1) {
            const pulse = effect('bard-chord:' + p.id, ring, material('#f5d783', 0.28));
            pulse.position.set(pos.x, 0.08, pos.z);
            pulse.scale.setScalar(0.5 + 4.5 * age);
          }
        }
        if (p.hero === 'berserker' && secondary) {
          const age = (visualTick - secondary.startTick) / 12;
          if (age >= 0 && age < 1) {
            const tint = material('#a6f58c', 0.86);
            const horizontal = effect('heal-horizontal:' + p.id, dashStreak, tint);
            const vertical = effect('heal-vertical:' + p.id, dashStreak, tint);
            horizontal.position.set(pos.x, 1.55 + 0.75 * age, pos.z);
            vertical.position.copy(horizontal.position);
            horizontal.scale.set(0.34, 0.09, 0.09);
            vertical.scale.set(0.09, 0.34, 0.09);
          }
        }
        if (dash) {
          const lean = Math.sin(Math.PI * dash.phase);
          pose = dash.phase < 0.3 ? 'windup' : dash.phase < 1 ? 'strike' : 'idle';
          actor.model.rotation.set(0.48 * lean, Math.PI / 2 - dash.aim, 0, 'YXZ');
          actor.model.position.y = -0.10 * lean;
          showDashTrail(p.id, dash);
        }
        if (p.id === map.playerId) {
          const projectilePose = localProjectiles.update(time, map, snapshot, p, pos, primaryAttempt);
          if (projectilePose) {
            pose = projectilePose;
            actor.model.rotation.y = Math.PI / 2 - primaryAttempt.aim;
          }
        }
        actor.model.geometry = actor.template.poses[pose];
      }
      // Remote flight and impacts share the buffered snapshot clock.
      const projectileTick = samples?.tick ?? tick;
      const ends = snapshot.projectileEnds ?? [];
      const shots = samples?.projectiles() ?? snapshot.projectiles ?? [];
      function showProjectile(key, p, x, z) {
        const isMagic = p.hero === 'mage';
        const isSound = p.hero === 'bard';
        const model = isSound
          ? { geometry: soundWave, material: material('#f5d783', 0.87) }
          : projectileModels.get(isMagic ? 'magic-bolt' : 'arrow');
        const shot = effect(key, model.geometry, model.material);
        shot.position.set(x, 0.64, z);
        shot.rotation.set(0, Math.PI / 2 - (p.aim ?? 0), 0);
        if (isSound) {
          shot.rotation.set(0, -(p.aim ?? 0), 0);
          shot.scale.setScalar(1.75);
        }
        if (isMagic) shot.rotateZ(time / 125);
      }
      for (const shot of shots) {
        if (!localProjectiles.owns(shot, map.playerId)) {
          showProjectile('shot:' + shot.id, shot, shot.x, shot.z);
        }
      }
      function showProjectileImpact(end, age) {
        const tint = end.hero === 'mage' ? '#ff704a' : end.hero === 'bard' ? '#f5d783' : '#f7e8b3';
        const impact = effect('projectile-end:' + end.id, ring, material(tint, 0.78));
        impact.position.set(end.x, 0.64, end.z);
        impact.scale.setScalar(0.12 + 0.38 * age);
      }
      for (const end of ends) {
        const age = (projectileTick - end.tick) / 7;
        if (age < 0 || age >= 1 || end.reason === 'expired' || localProjectiles.owns(end, map.playerId)) continue;
        showProjectileImpact(end, age);
      }
      localHit = localProjectiles.draw(time, map, snapshot, (shot, x, z) =>
        showProjectile('local-shot:' + shot.id, shot, x, z), showProjectileImpact);
      for (const m of [...(snapshot.meteors ?? []), ...recentMeteors]) {
        const key = m.ownerId + ':' + m.id;
        const landed = tick >= m.impactTick;
        const zone = effect('zone:' + key, disk, material(landed ? '#ff582f' : '#e43b32', landed ? 0.66 : 0.28));
        zone.position.set(m.x, 0.06, m.z);
        zone.scale.setScalar(m.radius);
        if (!landed) {
          const model = projectileModels.get('meteor');
          const rock = effect('meteor:' + key, model.geometry, model.material);
          rock.position.set(m.x, 0.5 + 8 * Math.max(0, m.impactTick - visualTick) / map.tickRate, m.z);
          rock.rotation.set(time / 290, time / 420, time / 650);
        }
      }

    }
    for (const [id, actor] of players) if (!seenPlayers.has(id)) removePlayer(id, actor);
    for (const [key, mesh] of transient) {
      if (frameSeen.has(key)) continue;
      effects.remove(mesh);
      transient.delete(key);
    }
    renderer.render(scene, camera);
    return localHit;
  }

  return { setMap, resetRound, groundPoint, render, ownsProjectile: shot => localProjectiles.owns(shot, map?.playerId), setModels: models => {
    templates = models.heroes;
    projectileModels = models.projectiles;
  } };
}
