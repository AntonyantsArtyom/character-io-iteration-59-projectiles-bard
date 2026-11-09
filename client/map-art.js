import * as THREE from './assets/three/three.module.min.js';

const unit = new THREE.BoxGeometry(1, 1, 1);
const matte = new THREE.MeshStandardMaterial({ roughness: 0.94 });
const accents = new THREE.MeshBasicMaterial();
const stone = ['#676966', '#74746d', '#777a73', '#828078', '#6d756a'];
const cobble = ['#5f5d50', '#6a6859', '#74705e', '#686658', '#7d7967'];
const soil = ['#3e4a36', '#46533b', '#505940', '#4a563e', '#566147'];
const foliage = ['#48613b', '#5f783f', '#738749', '#809551', '#455b38'];

function randomSequence() {
  let state = 91371;
  return () => {
    state = (Math.imul(state, 1664525) + 1013904223) >>> 0;
    return state / 4294967296;
  };
}

function batch(blocks, material, name) {
  const mesh = new THREE.InstancedMesh(unit, material, blocks.length);
  mesh.name = name;
  const transform = new THREE.Object3D();
  const tint = new THREE.Color();
  blocks.forEach(([x, y, z, w, h, d, color, yaw], i) => {
    transform.position.set(x, y, z);
    transform.scale.set(w, h, d);
    transform.rotation.y = yaw;
    transform.updateMatrix();
    mesh.setMatrixAt(i, transform.matrix);
    mesh.setColorAt(i, tint.set(color));
  });
  mesh.instanceMatrix.needsUpdate = true;
  mesh.instanceColor.needsUpdate = true;
  mesh.computeBoundingSphere();
  return mesh;
}

export function createCourtyard(map) {
  const random = randomSequence();
  const choose = colors => colors[Math.floor(random() * colors.length)];
  const opaque = [];
  const light = [];
  const block = (x, y, z, w, h, d, color, yaw = 0) => opaque.push([x, y, z, w, h, d, color, yaw]);
  const glow = (x, y, z, w, h, d, color) => light.push([x, y, z, w, h, d, color, 0]);
  const { min, max } = map.bounds;
  const width = max.x - min.x;
  const depth = max.z - min.z;
  const cx = (min.x + max.x) / 2;
  const cz = (min.z + max.z) / 2;
  const walls = map.walls ?? [];
  const contains = (wall, x, z, margin = 0) => x >= wall.min.x - margin && x <= wall.max.x + margin &&
    z >= wall.min.z - margin && z <= wall.max.z + margin;

  function isPath(x, z) {
    if (!map.zones?.length) return Math.abs(z - cz) < depth * 0.108;
    return map.zones.some(zone => Math.hypot(x - zone.x, z - zone.z) < zone.radius + 0.8 ||
      Math.abs(z - zone.z) < 2.1) ||
      Math.abs(x - cx) < 2.1 || Math.abs(Math.abs(x - cx) - width * 0.4) < 1.6;
  }

  block(cx, -0.28, cz, width + 0.3, 0.5, depth + 0.3, '#343b30');
  block(cx, -0.045, cz, width, 0.09, depth, '#3c4534');

  const columns = Math.ceil(width / 0.96);
  const rows = Math.ceil(depth / 0.76);
  const tileX = width / columns;
  const tileZ = depth / rows;
  for (let row = 0; row < rows; row++) {
    const z = min.z + (row + 0.5) * tileZ;
    for (let column = 0; column < columns; column++) {
      const x = min.x + (column + 0.5) * tileX;
      const paved = isPath(x, z);
      const rim = row === 0 || column === 0 || row === rows - 1 || column === columns - 1;
      const spawnPad = Math.abs(x - cx) / width > 0.37 && Math.abs(z - cz) / depth < 0.17;
      const padColors = x < cx ? ['#5a6875', '#606e7a', '#647381'] : ['#765b55', '#7e615a', '#84665d'];
      const color = rim ? '#6a6a55' : choose(spawnPad ? padColors : paved ? cobble : soil);
      const height = 0.017 + random() * 0.014;
      block(x, height / 2, z, tileX - (paved ? 0.034 : 0.009), height,
        tileZ - (paved ? 0.032 : 0.008), color);
      if (paved || rim || walls.some(wall => contains(wall, x, z, 0.55)) || random() > 0.23) continue;

      const ox = x + (random() - 0.5) * 0.35;
      const oz = z + (random() - 0.5) * 0.3;
      if (random() < 0.24) {
        const size = 0.11 + random() * 0.14;
        block(ox, 0.04 + size * 0.21, oz, size, size * 0.43, size * 0.84, choose(stone), random());
        block(ox + 0.1, 0.062, oz + 0.07, size * 0.55, 0.065, size * 0.52, '#7e816d', random());
      } else {
        for (let blade = 0; blade < 3; blade++) {
          const tall = 0.09 + random() * 0.18;
          block(ox + (blade - 1) * 0.075, 0.021 + tall / 2, oz + (blade % 2) * 0.07,
            0.043, tall, 0.051, choose(foliage), random());
          if (blade === 2 && random() < 0.09) {
            block(ox + 0.075, 0.039 + tall, oz, 0.065, 0.045, 0.065, '#baa66d');
          }
        }
      }
    }
  }

  for (const wall of walls) {
    const x = (wall.min.x + wall.max.x) / 2;
    const z = (wall.min.z + wall.max.z) / 2;
    const w = wall.max.x - wall.min.x;
    const d = wall.max.z - wall.min.z;
    block(x, 0.13, z, w, 0.26, d, '#4b5046');
    if (z > cz + depth * 0.16) {
      // Raised planters keep their full collision footprint visible.
      block(x, 0.4, z, w, 0.28, d, '#747161');
      block(x, 0.55, z, w - 0.28, 0.03, d - 0.28, '#524739');
      for (let i = 0; i < Math.floor(w * d * 2); i++) {
        const h = 0.16 + random() * 0.42;
        block(x + (random() - 0.5) * (w - 0.55), 0.56 + h / 2,
          z + (random() - 0.5) * (d - 0.55), 0.22, h, 0.22, choose(foliage), random());
      }
      continue;
    }
    if (z < cz - depth * 0.16) {
      // Gate piers flank an open passage. Caps stay inside the collider.
      block(x, 0.68, z, w - 0.28, 0.9, d - 0.28, '#6b6c64');
      block(x, 1.21, z, w, 0.17, d, '#939087');
      block(x, 1.40, z, w - 0.5, 0.22, d - 0.5, '#76776f');
      const side = x < cx ? -1 : 1;
      block(x - side * (w / 2 - 0.025), 0.83, z, 0.05, 0.49, 0.72,
        side < 0 ? '#5984ad' : '#aa5355');
      continue;
    }

    const alongX = w >= d;
    const length = alongX ? w : d;
    const thickness = alongX ? d : w;
    function brick(start, end, y, h, color) {
      const span = end - start;
      const offset = (start + end - length) / 2;
      block(x + (alongX ? offset : 0), y, z + (alongX ? 0 : offset),
        alongX ? span : thickness, h, alongX ? thickness : span, color);
    }
    // A continuous base gives the whole server collider a visible footprint.
    brick(0, length, 0.35, 0.36, '#51574c');
    for (let course = 0; course < 2; course++) {
      for (let start = 0; start < length - 0.04;) {
        const end = Math.min(length, start + 0.63 + random() * 0.55);
        if (end - start > 0.055 && (course === 0 || random() > 0.29)) {
          brick(start, end - 0.022, 0.61 + course * 0.23, 0.22, choose(stone));
        }
        start = end;
      }
    }
  }

  for (const side of [-1, 1]) {
    const teamX = cx + side * width * 0.42;
    const team = side < 0 ? '#5984ad' : '#aa5355';
    const border = side < 0 ? '#83b5d5' : '#d98076';
    // Inlaid banners and lanterns sit beyond the playable side edge.
    for (const lane of [-1, 1]) {
      const z = cz + lane * depth * 0.15;
      const x = cx + side * (width / 2 + 0.31);
      block(x, 0.65, z, 0.24, 1.3, 0.24, '#5d4e3e');
      block(x, 1.01, z, 0.075, 0.67, 0.64, team);
      block(x, 0.71, z, 0.085, 0.07, 0.64, border);
      block(x, 1.34, z, 0.38, 0.12, 0.38, '#676353');
      glow(x, 1.46, z, 0.18, 0.15, 0.18, '#ffce87');
    }
    for (let mark = -2; mark <= 2; mark++) {
      block(teamX + side * Math.abs(mark) * 0.34, 0.036, cz + mark * 0.46,
        0.5, 0.012, 0.43, mark === 0 ? border : team);
    }
  }
  const terrain = new THREE.Group();
  terrain.name = 'Fortress courtyard';
  terrain.add(batch(opaque, matte, 'Terrain blocks'), batch(light, accents, 'Lanterns'));
  return terrain;
}

export function clearCourtyard(root) {
  root.traverse(mesh => { if (mesh.isInstancedMesh) mesh.dispose(); });
  root.clear();
}
