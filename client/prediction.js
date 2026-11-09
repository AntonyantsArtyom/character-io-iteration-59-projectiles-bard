const clamp = (value, lo, hi) => Math.max(lo, Math.min(value, hi));

// Mirrors game.World.moveAxis. Use the same X-then-Z sweep as the server.
function moveAxis(position, delta, axis, across, map) {
  const radius = map.playerRadius;
  const start = position[axis];
  const side = position[across];
  let end = clamp(start + delta, map.bounds.min[axis] + radius, map.bounds.max[axis] - radius);
  for (const wall of map.walls) {
    const gap = Math.abs(side - clamp(side, wall.min[across], wall.max[across]));
    if (gap >= radius) continue;
    const margin = Math.sqrt(radius * radius - gap * gap);
    const min = wall.min[axis];
    const max = wall.max[axis];
    if (delta > 0 && start <= min && end > min - margin) end = min - margin;
    if (delta < 0 && start >= max && end < max + margin) end = max + margin;
  }
  return end;
}

function movePlayer(position, input, distance, map) {
  const scale = distance / Math.max(1, Math.hypot(input.moveX, input.moveZ));
  const x = moveAxis(position, input.moveX * scale, 'x', 'z', map);
  const z = moveAxis({ x, z: position.z }, input.moveZ * scale, 'z', 'x', map);
  return { x, z };
}

export function predictPlayer(player, pending, input, fraction, map, serverTick, players = []) {
  let position = { x: player.x, z: player.z };
  const step = map.moveSpeed / map.tickRate;
  const attack = player.attack;
  const windupMultiplier = attack?.windupMoveMultiplier ?? 1;
  const radius = map.windupEnemyRadius ?? 0;
  const enemies = windupMultiplier > 1 && radius > 0
    ? players.filter(other => other.hp > 0 && other.id !== player.id && (!player.team || other.team !== player.team))
    : [];
  const distanceAt = tick => {
    const inspiration = tick <= player.inspirationSpeedUntil ? (map.inspirationMoveMultiplier ?? 1.15) : 1;
    const nearEnemy = tick <= attack?.hitTick && enemies.some(enemy =>
      (enemy.invisibleUntil ?? 0) <= tick && (enemy.x - position.x) ** 2 + (enemy.z - position.z) ** 2 <= radius ** 2);
    const windup = nearEnemy ? windupMultiplier : 1;
    return step * Math.max(inspiration, windup);
  };
  let tick = serverTick;
  for (const command of pending) position = movePlayer(position, command, distanceAt(++tick), map);
  return movePlayer(position, input, distanceAt(tick + 1) * fraction, map);
}

// The server sends these once per connection. Keep them available to the render buffer.
export function mergeSnapshotEvents(snapshot, previous, tickRate) {
  if (!previous || snapshot.round?.number !== previous.round?.number) return;
  const oldest = snapshot.serverTick - 2 * tickRate;
  snapshot.projectileEnds = [
    ...(previous.projectileEnds ?? []).filter(end => end.tick > oldest),
    ...(snapshot.projectileEnds ?? []),
  ];
  if (snapshot.round?.restartTick && snapshot.round.restartTick === previous.round?.restartTick) {
    snapshot.roundResults ??= previous.roundResults;
  }
}

const delayMs = 120;
const maxFrames = 16;

export function createSnapshotBuffer() {
  const frames = [];
  let lastTarget = -Infinity;
  return {
    push(snapshot, arrivedAt) {
      if (frames.length && snapshot.serverTick <= frames.at(-1).snapshot.serverTick) return;
      frames.push({
        snapshot, arrivedAt,
        players: new Map(snapshot.players.map(p => [p.id, p])),
        projectiles: new Map((snapshot.projectiles ?? []).map(p => [p.id, p])),
      });
      if (frames.length > maxFrames) frames.shift();
    },
    reset() { frames.length = 0; lastTarget = -Infinity; },
    sample(now, tickRate) {
      if (frames.length < 2) return null;
      const tickMs = 1000 / tickRate;
      const offset = Math.min(...frames.map(f => f.arrivedAt - f.snapshot.serverTick * tickMs));
      const target = Math.min(frames.at(-1).snapshot.serverTick,
        Math.max(lastTarget, (now - offset - delayMs) / tickMs));
      lastTarget = target;
      let right = frames.findIndex(f => f.snapshot.serverTick >= target);
      if (right < 0) right = frames.length - 1;
      const left = Math.max(0, right - 1);
      const a = frames[left];
      const b = frames[right];
      const span = b.snapshot.serverTick - a.snapshot.serverTick;
      const mix = span ? Math.max(0, Math.min(1, (target - a.snapshot.serverTick) / span)) : 0;

      function sample(kind, current) {
        const start = a[kind].get(current.id);
        const end = b[kind].get(current.id);
        if (!start || !end) return current;
        if (kind === 'players') {
          // Never rewind across a spawn, invisibility transition, death or dash.
          let previous = start;
          for (let i = left + 1; i < frames.length; i++) {
            const next = frames[i].players.get(current.id);
            if (!next?.hp || !previous.hp || Math.hypot(next.x - previous.x, next.z - previous.z) > 2) {
              return current;
            }
            previous = next;
          }
        }
        return {
          x: start.x + (end.x - start.x) * mix,
          z: start.z + (end.z - start.z) * mix,
          aim: kind === 'players'
            ? start.aim + Math.atan2(Math.sin(end.aim - start.aim), Math.cos(end.aim - start.aim)) * mix
            : current.aim,
        };
      }
      const tick = a.snapshot.serverTick + mix * span;
      const ends = new Map((frames.at(-1).snapshot.projectileEnds ?? []).map(event => [event.id, event]));
      function projectiles() {
        const shots = [];
        for (const shot of (mix === 1 ? b : a).projectiles.values()) {
          const end = ends.get(shot.id);
          if (end && end.tick <= tick) continue;
          if (end && !b.projectiles.has(shot.id) && end.tick > a.snapshot.serverTick) {
            const progress = (tick - a.snapshot.serverTick) / (end.tick - a.snapshot.serverTick);
            shots.push({ ...shot, x: shot.x + (end.x - shot.x) * progress,
              z: shot.z + (end.z - shot.z) * progress });
          } else {
            shots.push({ ...shot, ...sample('projectiles', shot) });
          }
        }
        // Recover the launch segment before the first snapshot that contains the shot.
        for (const shot of b.projectiles.values()) {
          if (mix === 1 || a.projectiles.has(shot.id) || !(tick >= shot.launchTick) || ends.get(shot.id)?.tick <= tick) continue;
          const progress = (tick - shot.launchTick) / (b.snapshot.serverTick - shot.launchTick);
          shots.push({ ...shot, x: shot.launchX + (shot.x - shot.launchX) * progress,
            z: shot.launchZ + (shot.z - shot.launchZ) * progress });
        }
        // A close-range shot can end before it ever appears in a snapshot.
        for (const end of ends.values()) {
          if (a.projectiles.has(end.id) || b.projectiles.has(end.id) ||
              !(tick >= end.launchTick && tick < end.tick)) continue;
          const progress = (tick - end.launchTick) / (end.tick - end.launchTick);
          shots.push({ ...end, x: end.launchX + (end.x - end.launchX) * progress,
            z: end.launchZ + (end.z - end.launchZ) * progress });
        }
        return shots;
      }

      return {
        tick,
        player: p => sample('players', p),
        projectiles,
      };
    },
  };
}
