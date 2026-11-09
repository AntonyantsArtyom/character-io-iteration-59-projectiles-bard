import { createScene, loadModels } from "./scene.js";
import { createSnapshotBuffer, mergeSnapshotEvents, predictPlayer } from "./prediction.js";
const byId = id => document.getElementById(id);
const canvas = byId('arena');
const hitMarker = byId('hit-marker');
const pingLabel = byId('network-ping');
const resultsPanel = byId('round-results');
let resultsKey = '';
const status = byId('status');
const minimap = byId('minimap');
const controlHUD = byId('control-hud');
const zoneNames = ['A', 'B', 'C'];
const keys = new Set();
const movementKeys = new Set(['KeyW', 'KeyA', 'KeyS', 'KeyD', 'ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight']);

const teamColors = { blue: '#65baff', red: '#ff737d' };
const teamNames = { blue: 'Синие', red: 'Красные' };

let socket = null;
let map = null;
let snapshot = null;
let seq = 0;
let actionId = 0;
let primaryHeld = false;
let primaryAttempt = null;
let aim = 0;
let pointer = null;
const snapshotBuffer = createSnapshotBuffer();
let lastSnapshotAt = null;
let localFeedback = null;
let displayedTick = 0;
let pending = [];
let queuedActions = [];
let predicted = null;
let lastInputTime = null;
let inputElapsed = 0;
let inputX = 0, inputZ = 0;
let view;
try {
  view = createScene(canvas, byId('labels'));
  view.setModels(await loadModels());
  byId('connect').disabled = false;
  status.textContent = '';
} catch (error) {
  status.textContent = '3D не загрузилось. Открой index.html через локальный HTTP-сервер и проверь WebGL.';
  console.error(error);
}
let recentMeteors = [];

byId('nick').value = 'Игрок ' + Math.floor(100 + Math.random() * 900);

const picker = byId('hero-picker');
const heroButtons = [...picker.querySelectorAll('[data-hero]')];
let selectedHero = heroButtons[0];
byId('choose-hero').addEventListener('click', () => picker.showModal());
for (const button of heroButtons) {
  button.addEventListener('click', () => {
    selectedHero.setAttribute('aria-pressed', 'false');
    selectedHero = button;
    button.setAttribute('aria-pressed', 'true');
    byId('selected-hero').textContent = button.querySelector('strong').textContent;
    byId('selected-icon').textContent = button.querySelector('.hero-icon').textContent;
    byId('choose-hero').style.setProperty('--hero', button.style.getPropertyValue('--hero'));
    picker.close();
  });
}

byId('fullscreen').addEventListener('click', async () => {
  try {
    if (document.fullscreenElement) await document.exitFullscreen();
    else await byId('viewport').requestFullscreen();
  } catch (error) {
    console.warn('Не удалось переключить полноэкранный режим', error);
  }
  canvas.focus();
});
document.addEventListener('fullscreenchange', () => {
  byId('fullscreen').textContent = document.fullscreenElement ? 'Свернуть экран' : 'Во весь экран';
});


function localPlayer() {
  return snapshot?.players.find(player => player.id === map?.playerId);
}

function updateAim(me) {
  if (me && pointer && view) {
    const target = view.groundPoint(pointer.x, pointer.y);
    if (!target) return;
    const dx = target.x - me.x;
    const dz = target.z - me.z;
    if (dx || dz) aim = Math.atan2(dz, dx);
  }
}

function advanceMovement(time) {
  const elapsed = lastInputTime === null ? 0 : Math.max(0, time - lastInputTime);
  lastInputTime = Math.max(lastInputTime ?? time, time);
  const me = localPlayer();
  if (!me?.hp || snapshot?.round?.restartTick || socket?.readyState !== WebSocket.OPEN ||
      lastSnapshotAt === null || time - lastSnapshotAt > 750) {
    inputElapsed = inputX = inputZ = 0;
    queuedActions = [];
    predicted = null;
    return;
  }

  const right = keys.has('KeyD') || keys.has('ArrowRight');
  const left = keys.has('KeyA') || keys.has('ArrowLeft');
  const down = keys.has('KeyS') || keys.has('ArrowDown');
  const up = keys.has('KeyW') || keys.has('ArrowUp');
  const x = Number(right) - Number(left);
  const z = Number(down) - Number(up);
  const length = Math.max(1, Math.hypot(x, z));
  const tickMs = 1000 / map.tickRate;
  let remaining = Math.min(elapsed, tickMs * 3);
  updateAim(predicted ?? me);

  while (remaining > 0) {
    const portion = Math.min(remaining, tickMs - inputElapsed);
    inputX += x / length * portion;
    inputZ += z / length * portion;
    inputElapsed += portion;
    remaining -= portion;
    if (inputElapsed < tickMs - 1e-6) continue;
    if (socket.bufferedAmount <= 4096) {
      const command = { type: 'move_step', seq: ++seq,
        moveX: inputX / tickMs, moveZ: inputZ / tickMs, aim };
      if (queuedActions.length) {
        command.actions = queuedActions;
        if (queuedActions.some(action => action.id === primaryAttempt?.id)) primaryAttempt.seq = command.seq;
        queuedActions = [];
      }
      socket.send(JSON.stringify(command));
      pending.push(command);
      if (pending.length > 16) pending.shift();
    }
    inputElapsed = inputX = inputZ = 0;
  }

  const preview = {
    moveX: inputElapsed ? inputX / inputElapsed : 0,
    moveZ: inputElapsed ? inputZ / inputElapsed : 0,
  };
  predicted = { ...predictPlayer(me, pending, preview, inputElapsed / tickMs, map, snapshot.serverTick, snapshot.players), aim };
}

function clearKeys() {
  primaryHeld = false;
  advanceMovement(performance.now());
  keys.clear();
  lastInputTime = null;
  inputElapsed = inputX = inputZ = 0;
}

window.addEventListener('keydown', event => {
  if (!map || !movementKeys.has(event.code) || event.target.closest?.('input, select, button, textarea')) return;
  event.preventDefault();
  if (!keys.has(event.code)) {
    advanceMovement(performance.now());
    keys.add(event.code);
  }
});
window.addEventListener('keyup', event => {
  if (!keys.has(event.code)) return;
  advanceMovement(performance.now());
  keys.delete(event.code);
});
window.addEventListener('blur', clearKeys);
document.addEventListener('visibilitychange', () => {
  if (document.hidden) clearKeys();
});
byId('join').addEventListener('focusin', clearKeys);
function trackPointer(event) {
  pointer = { x: event.clientX, y: event.clientY };
}
canvas.addEventListener('pointermove', trackPointer);
canvas.addEventListener('contextmenu', event => event.preventDefault());

function sendAttack(secondary) {
  const now = performance.now();
  if (!view || snapshot?.round?.restartTick || lastSnapshotAt === null || now - lastSnapshotAt > 750 ||
      socket?.readyState !== WebSocket.OPEN || socket.bufferedAmount > 4096) return;
  const me = localPlayer();
  const button = secondary ? 'secondary' : 'primary';
  if (!(me?.hp > 0) || queuedActions.some(action => action.button === button)) return;
  const tick = snapshot.serverTick;
  if (secondary) {
    if (me.secondaryReadyTick > tick || (me.hero === 'berserker' && me.hp >= me.maxHp)) return;
  } else {
    if (me.primaryReadyTick > tick) return;
    if (primaryAttempt && (me.attack?.id ?? 0) < primaryAttempt.id &&
        tick - primaryAttempt.tick < 4) return;
  }

  updateAim(predicted ?? me);
  const action = { button,
    id: ++actionId, aim, viewTick: displayedTick };
  if (secondary && me.hero === 'mage') {
    if (!pointer) return;
    action.target = view.groundPoint(pointer.x, pointer.y);
    if (!action.target) return;
  }
  queuedActions.push(action);
  if (!secondary) primaryAttempt = { id: action.id, tick, aim, startedAt: now };
  if (!localFeedback || now >= localFeedback.until) {
    localFeedback = { id: action.id, secondary, aim, startedAt: now, until: now + 160 };
  }
}

canvas.addEventListener('mousedown', event => {
  if (event.button !== 0 && event.button !== 2) return;
  trackPointer(event);
  if (event.button === 0) primaryHeld = true;
  sendAttack(event.button === 2);
});
canvas.addEventListener('pointerdown', event => {
  if (event.pointerType === 'mouse' || (event.button !== 0 && event.button !== 2)) return;
  event.preventDefault();
  trackPointer(event);
  sendAttack(event.button === 2);
});
window.addEventListener('mousemove', event => {
  if (!primaryHeld) return;
  if (!(event.buttons & 1)) primaryHeld = false;
  else trackPointer(event);
});
window.addEventListener('mouseup', event => {
  if (event.button === 0) primaryHeld = false;
});
window.addEventListener('pointercancel', () => { primaryHeld = false; });
canvas.addEventListener('pointerleave', () => { if (!primaryHeld) pointer = null; });

function connectToServer(event) {
  event.preventDefault();
  if (socket || byId('connect').disabled) return;
  const nick = byId('nick').value.trim();
  if (!nick) return;
  const address = new URLSearchParams(location.search).get('server') || '/ws';
  let connection;
  try {
    connection = new WebSocket(address);
  } catch {
    status.textContent = 'Укажи корректный адрес ws:// или wss://.';
    return;
  }
  socket = connection;
  byId('connect').disabled = true;
  byId('choose-hero').disabled = true;
  status.textContent = 'Подключение…';
  let problem = '';
  const hero = selectedHero.dataset.hero;
  let pingTimer;
  let pingID = 0;
  let sentAt = null;

  function probePing() {
    const now = performance.now();
    if (connection.readyState !== WebSocket.OPEN) return;
    if (sentAt !== null) {
      if (now - sentAt < 5000) return;
      pingLabel.textContent = 'Пинг —';
      sentAt = null;
    }
    if (connection.bufferedAmount > 4096) return;
    sentAt = now;
    connection.send(JSON.stringify({ type: 'ping', id: ++pingID }));
  }

  connection.addEventListener('open', () => {
    connection.send(JSON.stringify({ type: 'join', nick, hero, compactSnapshots: true }));
  });
  connection.addEventListener('message', event => {
    const message = JSON.parse(event.data);
    if (message.type === 'welcome') {
      map = message;
      view.setMap(map);
      controlHUD.hidden = minimap.hidden = !map.zones?.length;
      seq = 0;
      actionId = 0;
      primaryHeld = false;
      primaryAttempt = null;
      aim = 0;
      pending = [];
      queuedActions = [];
      predicted = null;
      lastInputTime = null;
      status.textContent = 'Загружаем матч…';
      if (pingTimer === undefined) {
        probePing();
        pingTimer = setInterval(probePing, 2000);
      }
    } else if (message.type === 'pong' && message.id === pingID && sentAt !== null) {
      const rtt = Math.round(performance.now() - sentAt);
      pingLabel.textContent = `Пинг ${rtt} мс`;
      sentAt = null;
    } else if (message.type === 'snapshot' && map) {
      applySnapshot(message);
    } else if (message.type === 'error') {
      const errors = { arena_full: 'Арена заполнена.', invalid_nick: 'Проверь ник.', invalid_hero: 'Неизвестный герой.' };
      problem = errors[message.code] ?? 'Ошибка сервера: ' + message.code;
      status.textContent = problem;
    }
  });
  connection.addEventListener('error', () => {
    problem ||= 'Арена недоступна. Попробуй подключиться ещё раз.';
    status.textContent = problem;
  });
  connection.addEventListener('close', () => {
    clearInterval(pingTimer);
    pingLabel.textContent = 'Пинг —';
    pending = [];
    queuedActions = [];
    predicted = null;
    lastInputTime = null;
    inputElapsed = inputX = inputZ = 0;
    keys.clear();
    primaryHeld = false;
    primaryAttempt = null;
    pointer = null;
    recentMeteors = [];
    snapshotBuffer.reset();
    lastSnapshotAt = null;
    localFeedback = null;
    displayedTick = 0;
    socket = map = snapshot = null;
    view.setMap(null);
    controlHUD.hidden = minimap.hidden = true;
    byId('connect').disabled = false;
    byId('choose-hero').disabled = false;
    byId('game-hud').hidden = true;
    resultsPanel.hidden = true;
    resultsKey = '';
    byId('lobby').hidden = false;
    status.textContent = problem || '';
    byId('connect').focus();
  });
}

byId('join').addEventListener('submit', connectToServer);
byId('disconnect').addEventListener('click', () => socket?.close());
byId('results-exit').addEventListener('click', () => socket?.close());
function applySnapshot(message) {
  const now = performance.now();
  if (snapshot && message.serverTick <= snapshot.serverTick) return;
  const newRound = snapshot && message.round?.number !== snapshot.round?.number;
  const roundEnded = message.round?.restartTick;
  if (newRound || roundEnded) {
    pending = [];
    queuedActions = [];
    predicted = null;
    lastInputTime = null;
    inputElapsed = inputX = inputZ = 0;
    localFeedback = null;
    recentMeteors = [];
  }
  if (newRound) {
    primaryAttempt = null;
    primaryHeld = false;
    keys.clear();
    snapshotBuffer.reset();
    view.resetRound();
  }
  if (lastSnapshotAt !== null && now - lastSnapshotAt > 750) {
    snapshotBuffer.reset();
    pending = [];
    queuedActions = [];
  }
  if (map.compactSnapshots) mergeSnapshotEvents(message, snapshot, map.tickRate);
  snapshotBuffer.push(message, now);
  lastSnapshotAt = now;
  const meteors = message.meteors ?? [];
  recentMeteors = recentMeteors.concat((newRound || roundEnded ? [] : snapshot?.meteors ?? []).filter(meteor =>
    meteor.impactTick <= message.serverTick &&
    !meteors.some(active => active.id === meteor.id && active.ownerId === meteor.ownerId)
  )).filter(meteor => message.serverTick < meteor.impactTick + 6);
  snapshot = message;
  const me = localPlayer();
  updateControlHUD(message, me);
  pending = me?.hp ? pending.filter(command => command.seq > message.lastInputSeq) : [];
  if (!me?.hp) {
    primaryAttempt = null;
    localFeedback = null;
    queuedActions = [];
  }
  if (byId('game-hud').hidden) {
    byId('lobby').hidden = true;
    byId('game-hud').hidden = false;
    byId('player-hero').textContent = selectedHero.querySelector('strong').textContent;
    byId('ability-name').textContent = selectedHero.dataset.ability;
    canvas.focus();
  }
  if (me) {
    const bar = byId('health-bar');
    bar.max = me.maxHp;
    bar.value = me.hp;
    byId('player-hp').textContent = me.hp + ' / ' + me.maxHp;
    const inspiration = byId('inspiration-state');
    const speedTime = ((me.inspirationSpeedUntil ?? 0) - message.serverTick) / map.tickRate;
    inspiration.hidden = !me.inspirationCharges && speedTime <= 0;
    const bonuses = [];
    if (me.inspirationCharges) bonuses.push('ЛКМ ×' + me.inspirationCharges);
    if (speedTime > 0) bonuses.push('Бег ' + speedTime.toFixed(1) + ' с');
    inspiration.textContent = bonuses.join(' · ');
    const cooldown = (me.secondaryReadyTick - message.serverTick) / map.tickRate;
    byId('ability-state').textContent = cooldown > 0 ? cooldown.toFixed(1) + ' с' : 'Готова';
  }

  updateRoundResults(message);
  const notice = byId('match-notice');
  if (!roundEnded && me?.hp === 0 && me.respawnTick) {
    const seconds = Math.max(0, me.respawnTick - message.serverTick) / map.tickRate;
    notice.textContent = 'Возрождение через ' + seconds.toFixed(1) + ' с';
  } else {
    notice.textContent = '';
  }
  notice.hidden = !notice.textContent;
}

function updateRoundResults(message) {
  const { round } = message;
  if (!round?.restartTick) {
    if (!resultsPanel.hidden) {
      resultsPanel.hidden = true;
      resultsKey = '';
      canvas.focus();
    }
    return;
  }
  const opening = resultsPanel.hidden;
  resultsPanel.hidden = false;
  if (opening) {
    primaryHeld = false;
    keys.clear();
  }
  byId('results-round').textContent = 'Итоги раунда ' + round.number;
  byId('results-title').textContent = round.winner
    ? (round.winner === map.team ? 'Победа' : 'Поражение') + ' · ' + teamNames[round.winner]
    : 'Ничья';
  byId('results-blue').textContent = round.blue;
  byId('results-red').textContent = round.red;
  const seconds = Math.ceil(Math.max(0, round.restartTick - message.serverTick) / map.tickRate);
  byId('results-timer').textContent = `Следующий раунд через ${seconds} с`;
  const rows = message.roundResults ?? [];
  const key = JSON.stringify(rows);
  if (key !== resultsKey) {
    resultsKey = key;
    const ordered = [...rows].sort((a, b) => a.team.localeCompare(b.team) ||
      b.captures - a.captures || b.kills - a.kills || a.nick.localeCompare(b.nick));
    byId('results-players').replaceChildren(...ordered.map(player => {
      const row = document.createElement('tr');
      if (player.id === map.playerId) row.dataset.own = '';
      const name = document.createElement('td');
      name.className = player.team;
      name.textContent = player.nick + (player.id === map.playerId ? ' · ты' : '');
      const role = document.createElement('small');
      const hero = heroButtons.find(button => button.dataset.hero === player.hero);
      role.textContent = (hero?.querySelector('strong')?.textContent ?? player.hero) +
        (player.bot ? ' · бот' : '');
      name.append(role);
      row.append(name);
      for (const value of [player.kills, player.deaths, player.captures, player.inspiredAttacks]) {
        const cell = document.createElement('td');
        cell.textContent = value;
        row.append(cell);
      }
      return row;
    }));
  }
  if (opening) byId('results-title').focus();
}

function updateControlHUD(message, me) {
  if (!map.zones?.length || !message.round) return;
  const { blue, red, number } = message.round;
  byId('blue-score').textContent = blue;
  byId('red-score').textContent = red;
  byId('round-score').textContent = `Раунд ${number} · до ${map.winScore}`;
  const chips = byId('control-zones');
  if (chips.children.length !== map.zones.length) {
    chips.replaceChildren(...map.zones.map(() => document.createElement('span')));
  }
  for (const [i, chip] of [...chips.children].entries()) {
    const state = message.zones?.[i];
    const capture = state?.contested ? ' · бой' : state?.capturing ? ' · ' + Math.floor(state.progress * 100) + '%' : '';
    chip.className = 'zone ' + (state?.contested ? '' : state?.capturing || state?.owner || '');
    chip.textContent = zoneNames[i] + capture;
    chip.title = teamNames[state?.owner] ?? 'Свободна';
  }

  const ctx = minimap.getContext('2d');
  const { min, max } = map.bounds;
  const sx = minimap.width / (max.x - min.x), sz = minimap.height / (max.z - min.z);
  const x = value => (value - min.x) * sx;
  const z = value => (value - min.z) * sz;
  ctx.fillStyle = '#222d29';
  ctx.fillRect(0, 0, minimap.width, minimap.height);
  ctx.fillStyle = '#7c8178';
  for (const wall of map.walls) {
    ctx.fillRect(x(wall.min.x), z(wall.min.z),
      (wall.max.x - wall.min.x) * sx, (wall.max.z - wall.min.z) * sz);
  }
  ctx.font = 'bold 14px system-ui';
  ctx.textAlign = 'center';
  ctx.textBaseline = 'middle';
  for (const [i, zone] of map.zones.entries()) {
    const state = message.zones?.[i];
    ctx.fillStyle = state?.contested ? '#ffcd65' : teamColors[state?.owner] ?? '#c8b98b';
    ctx.beginPath();
    ctx.ellipse(x(zone.x), z(zone.z), zone.radius * sx, zone.radius * sz, 0, 0, Math.PI * 2);
    ctx.fill();
    ctx.fillStyle = '#1b2023';
    ctx.fillText(zoneNames[i], x(zone.x), z(zone.z));
  }
  if (me) {
    ctx.fillStyle = '#fff';
    ctx.beginPath();
    ctx.arc(x(me.x), z(me.z), 3, 0, Math.PI * 2);
    ctx.fill();
  }
}

function draw(time) {
  requestAnimationFrame(draw);
  advanceMovement(time);
  const samples = map ? snapshotBuffer.sample(time, map.tickRate) : null;
  displayedTick = Math.round(samples?.tick ?? snapshot?.serverTick ?? 0);
  const projectileTick = samples?.tick ?? snapshot?.serverTick ?? 0;
  if (primaryHeld) sendAttack(false);
  const localHit = view.render(snapshot, recentMeteors, time, predicted, samples, localFeedback, primaryAttempt);
  const hit = localHit || snapshot?.projectileEnds?.some(end => end.ownerId === map?.playerId &&
    !view.ownsProjectile(end) &&
    end.reason === 'hit' && projectileTick >= end.tick && projectileTick < end.tick + 6);
  hitMarker.hidden = !(pointer && hit);
  if (!hitMarker.hidden) {
    hitMarker.style.left = pointer.x + 'px';
    hitMarker.style.top = pointer.y + 'px';
  }
}
if (!byId('connect').disabled) requestAnimationFrame(draw);
