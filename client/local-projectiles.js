// The server owns collisions and damage. These projectiles only represent local input.
export const muzzleOffset = 0.48;

function rayAxis(start, delta, min, max) {
  if (!delta) return start >= min && start <= max ? [-Infinity, Infinity] : [Infinity, -Infinity];
  const a = (min - start) / delta, b = (max - start) / delta;
  return [Math.min(a, b), Math.max(a, b)];
}

function flightLimit(position, dx, dz, map, reach) {
  const [, xExit] = rayAxis(position.x, dx, map.bounds.min.x, map.bounds.max.x);
  const [, zExit] = rayAxis(position.z, dz, map.bounds.min.z, map.bounds.max.z);
  let limit = Math.min(reach, xExit, zExit);
  for (const { min, max } of map.walls) {
    const [xEnter, xLeave] = rayAxis(position.x, dx, min.x, max.x);
    const [zEnter, zLeave] = rayAxis(position.z, dz, min.z, max.z);
    const entry = Math.max(0, xEnter, zEnter);
    if (entry <= Math.min(xLeave, zLeave)) limit = Math.min(limit, entry);
  }
  return Math.max(0, limit);
}

export function projectileSpec(map, hero) {
  return map.projectilePrediction?.[hero] ?? (hero === 'archer' ? map.archerProjectile : undefined);
}

export function createLocalProjectiles() {
  const projectiles = new Map();
  let latest = null;
  function owns(shot, playerId) {
    return shot.ownerId === playerId && projectiles.has(shot.actionId);
  }
  return {
    reset() { projectiles.clear(); latest = null; },
    owns,
    update(time, map, snapshot, player, position, attempt) {
      const spec = projectileSpec(map, player.hero);
      if (!spec) return null;
      if (!player.hp || !attempt || snapshot.round?.restartTick) return null;

      const acknowledged = attempt.seq && attempt.seq <= snapshot.lastInputSeq;
      const confirmed = player.attack?.id === attempt.id ||
        snapshot.projectileEnds?.some(end => end.ownerId === player.id && end.actionId === attempt.id);
      if (acknowledged && !confirmed) {
        const projectile = projectiles.get(attempt.id);
        if (projectile && !projectile.confirmed) projectile.rejected = true;
        if (latest === projectile) latest = null;
        return null;
      }

      const tickMs = 1000 / map.tickRate;
      const releaseAt = attempt.startedAt + spec.windup * tickMs;
      if (time < releaseAt) return 'windup';
      if (latest?.id !== attempt.id) {
        const dx = Math.cos(attempt.aim), dz = Math.sin(attempt.aim);
        const limit = flightLimit(position, dx, dz, map, spec.reach);
        const offset = Math.min(muzzleOffset, limit);
        latest = { id: attempt.id, hero: player.hero, speed: spec.speed, aim: attempt.aim, startedAt: time, dx, dz,
          x: position.x + dx * offset, z: position.z + dz * offset, limit: limit - offset };
        projectiles.set(latest.id, latest);
      }
      latest.confirmed ||= confirmed;
      return time - latest.startedAt < 5 * tickMs ? 'strike' : null;
    },
    draw(time, map, snapshot, showProjectile, showImpact) {
      if (snapshot.round?.restartTick) return false;
      for (const projectile of projectiles.values()) {
        if (time - projectile.startedAt > 3000) projectiles.delete(projectile.id);
      }
      const ends = new Map((snapshot.projectileEnds ?? [])
        .filter(end => owns(end, map.playerId)).map(end => [end.actionId, end]));
      let hit = false;
      for (const projectile of projectiles.values()) {
        if (projectile.rejected) continue;
        const end = ends.get(projectile.id);
        const distance = (time - projectile.startedAt) * projectile.speed / 1000;
        const endDistance = end
          ? Math.max(0, (end.x - projectile.x) * projectile.dx + (end.z - projectile.z) * projectile.dz)
          : Infinity;
        const limit = Math.min(projectile.limit, endDistance);
        if (distance < limit) {
          showProjectile(projectile, projectile.x + projectile.dx * distance, projectile.z + projectile.dz * distance);
        }
        if (end && distance >= limit) {
          projectile.impactAt ??= time;
          const age = (time - projectile.impactAt) * map.tickRate / 7000;
          if (age < 1 && end.reason !== 'expired') showImpact(end, age);
          hit ||= end.reason === 'hit' && age < 6 / 7;
        }
      }
      return hit;
    },
  };
}
