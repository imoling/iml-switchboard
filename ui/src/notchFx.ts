/** 刘海动画的粒子层：一次请求是一来一回。智能体把请求发给模型，光从左耳飞到右耳；模型做完把结果发回来，光从右耳飞回左耳。
 * 光走在刘海下方的能量带里，只在有动静时跑逐帧循环 */

export type NotchGeom = { width: number; height: number; centerX: number; band: number; logoY: number };

export type Pt = { x: number; y: number };

const TURN = 0.2; // 路线两头从耳朵拐进能量带所占的比例

/** 左右耳的标志中心 */
export const earX = (g: NotchGeom, side: -1 | 1) => g.centerX + side * (g.width / 2 + 21);

/** 粒子路线：左耳 → 能量带 → 右耳。t 从 0 到 1 */
export function routePoint(g: NotchGeom, t: number): Pt {
  const [lx, rx] = [earX(g, -1), earX(g, 1)];
  const by = g.height + g.band / 2;
  const curve = (a: Pt, c: Pt, b: Pt, u: number): Pt => ({
    x: (1 - u) ** 2 * a.x + 2 * (1 - u) * u * c.x + u ** 2 * b.x,
    y: (1 - u) ** 2 * a.y + 2 * (1 - u) * u * c.y + u ** 2 * b.y,
  });
  const inX = lx + 30, outX = rx - 30;
  const edge = g.logoY + 14; // 从标志下沿进出，不压在标志上
  if (t < TURN) return curve({ x: lx, y: edge }, { x: lx, y: by }, { x: inX, y: by }, t / TURN);
  if (t > 1 - TURN) return curve({ x: outX, y: by }, { x: rx, y: by }, { x: rx, y: edge }, (t - (1 - TURN)) / TURN);
  return { x: inX + ((t - TURN) / (1 - 2 * TURN)) * (outX - inX), y: by };
}

type Mote = { t: number; dir: 1 | -1; wait: number; speed: number; size: number; drift: number; phase: number; color: string };
type Spark = { x: number; y: number; vx: number; vy: number; life: number; max: number; size: number; color: string };
type Ring = { x: number; y: number; r: number; life: number; color: string };
type Comet = { t: number; to: number; size: number; color: string; onArrive: () => void };

/** 粒子的贴图：白热的芯，外面一圈颜色，渐隐。预先画好，每帧贴图比实时算发光便宜得多 */
function glowSprite(color: string): HTMLCanvasElement {
  const c = document.createElement('canvas');
  c.width = c.height = 64;
  const x = c.getContext('2d')!;
  const g = x.createRadialGradient(32, 32, 0, 32, 32, 32);
  g.addColorStop(0, '#fff');
  g.addColorStop(0.08, '#fff');
  g.addColorStop(0.24, color);
  g.addColorStop(1, 'transparent');
  x.fillStyle = g;
  x.fillRect(0, 0, 64, 64);
  return c;
}

export class NotchFx {
  private ctx: CanvasRenderingContext2D;
  private motes: Mote[] = [];
  private sparks: Spark[] = [];
  private rings: Ring[] = [];
  private comets: Comet[] = [];
  private sprites = new Map<string, HTMLCanvasElement>();
  private raf = 0;
  private last = 0;
  private outbound = 0; // 还在去模型路上的请求
  private replies: (() => void)[] = []; // 请求还没到右耳就做完了（很快的调用），回程要排在到达之后

  constructor(private canvas: HTMLCanvasElement, private geom: () => NotchGeom) {
    this.ctx = canvas.getContext('2d')!;
  }

  private sprite(color: string) {
    let s = this.sprites.get(color);
    if (!s) { s = glowSprite(color); this.sprites.set(color, s); }
    return s;
  }

  private burst(p: Pt, color: string, n: number, power = 1) {
    for (let i = 0; i < n; i++) {
      const a = (i / n) * Math.PI * 2 + Math.random() * 0.5;
      const v = (40 + Math.random() * 120) * power;
      const max = 0.3 + Math.random() * 0.35;
      this.sparks.push({ x: p.x, y: p.y, vx: Math.cos(a) * v, vy: Math.sin(a) * v, life: max, max, size: 3 + Math.random() * 5, color });
    }
    this.rings.push({ x: p.x, y: p.y, r: 8, life: 1, color });
    this.wake();
  }

  /** 一束光从 from 飞到 1 - from：一颗亮的彗星带几颗碎光跟着 */
  private fly(from: number, to: number, color: string, onArrive: () => void) {
    const dir = to > from ? 1 : -1;
    this.comets.push({ t: from, to, size: 12, color, onArrive });
    for (let i = 1; i <= 7; i++) {
      this.motes.push({ t: from, dir, wait: i * 0.03, speed: 1.9 + Math.random() * 0.5, size: 2.5 + Math.random() * 3, drift: 0.8 + Math.random() * 1.6, phase: Math.random() * 6.28, color });
    }
    this.wake();
  }

  private at(side: -1 | 1): Pt {
    const g = this.geom();
    return { x: earX(g, side), y: g.logoY };
  }

  /** 智能体发起请求：左耳发力，光飞到右耳，模型接住 */
  request(color: string) {
    this.burst(this.at(-1), color, 8, 0.5);
    this.outbound++;
    this.fly(0, 1, color, () => {
      this.burst(this.at(1), color, 12, 0.7);
      this.outbound--;
      if (this.outbound === 0) this.replies.splice(0).forEach((go) => go());
    });
  }

  /** 模型把结果发回：光飞回左耳，在智能体这里炸开。路由就没选到模型时，光从半路折返 */
  reply(color: string, ok: boolean, reached: boolean) {
    const go = () => this.fly(reached ? 1 : 0.5, 0, color, () => this.burst(this.at(-1), color, ok ? 22 : 30, ok ? 1 : 1.3));
    if (this.outbound > 0) this.replies.push(go); else go();
  }

  stop() { cancelAnimationFrame(this.raf); this.raf = 0; }

  private wake() {
    if (this.raf) return;
    this.last = performance.now();
    this.raf = requestAnimationFrame(this.frame);
  }

  private frame = (now: number) => {
    const dt = Math.min(0.05, (now - this.last) / 1000);
    this.last = now;
    const { ctx, canvas } = this;
    const g = this.geom();
    const scale = canvas.width / canvas.clientWidth || 1;
    ctx.setTransform(scale, 0, 0, scale, 0, 0);
    // 不清屏，只擦掉一部分，上一帧的亮点留下来就是拖尾
    ctx.globalCompositeOperation = 'destination-out';
    ctx.fillStyle = 'rgba(0,0,0,.28)';
    ctx.fillRect(0, 0, canvas.clientWidth, canvas.clientHeight);
    ctx.globalCompositeOperation = 'lighter';

    const draw = (color: string, x: number, y: number, size: number, alpha = 1) => {
      ctx.globalAlpha = alpha;
      ctx.drawImage(this.sprite(color), x - size, y - size, size * 2, size * 2);
    };
    this.motes = this.motes.filter((m) => {
      if (m.wait > 0) { m.wait -= dt; return true; }
      m.t += m.dir * m.speed * dt;
      if (m.t <= 0 || m.t >= 1) return false;
      const p = routePoint(g, m.t);
      draw(m.color, p.x, p.y + Math.sin(m.t * 18 + m.phase) * m.drift, m.size, 0.7); // 亮度别叠满，叠到白色就看不出是哪个渠道了
      return true;
    });
    this.comets = this.comets.filter((c) => {
      const steps = 4; // 一帧走得远，补几个点才连成一道
      const dir = c.to > c.t ? 1 : -1;
      for (let i = 0; i < steps; i++) {
        c.t += (dir * dt * 2.1) / steps;
        if ((c.t - c.to) * dir > 0) c.t = c.to;
        const p = routePoint(g, c.t);
        draw(c.color, p.x, p.y, c.size * (1 - i * 0.08));
      }
      if (c.t !== c.to) return true;
      c.onArrive();
      return false;
    });
    this.sparks = this.sparks.filter((s) => {
      s.life -= dt;
      if (s.life <= 0) return false;
      s.x += s.vx * dt; s.y += s.vy * dt;
      s.vx *= 0.94; s.vy *= 0.94;
      const k = s.life / s.max;
      draw(s.color, s.x, s.y, s.size * k, k);
      return true;
    });
    this.rings = this.rings.filter((r) => {
      r.life -= dt * 2.6;
      if (r.life <= 0) return false;
      r.r += dt * 110;
      ctx.globalAlpha = r.life * 0.7;
      ctx.strokeStyle = r.color;
      ctx.lineWidth = 2 * r.life + 0.5;
      ctx.beginPath(); ctx.arc(r.x, r.y, r.r, 0, Math.PI * 2); ctx.stroke();
      return true;
    });
    ctx.globalAlpha = 1;

    const alive = this.motes.length || this.comets.length || this.sparks.length || this.rings.length;
    if (alive) { this.raf = requestAnimationFrame(this.frame); return; }
    // 都走完了：擦干净再停，不留残影，也不留逐帧循环
    ctx.globalCompositeOperation = 'source-over';
    ctx.clearRect(0, 0, canvas.clientWidth, canvas.clientHeight);
    this.raf = 0;
  };
}
