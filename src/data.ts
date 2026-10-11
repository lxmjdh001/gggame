export interface Game { id: string; name: string; english: string; subtitle: string; color: string; accent: string; balls: string[]; tag?: string; hot: boolean; rows: number; max: number; zero?: boolean }
export const games: Game[] = [
  { id: 'ssc', name: '时时彩', english: 'SHISHICAI', subtitle: '经典玩法 · 每刻精彩', color: '#ffead8', accent: '#ee7d35', balls: ['8', '3', '5'], tag: '热门', hot: true, rows: 5, max: 9, zero: true },
  { id: 'ffc', name: '分分彩', english: 'FAST LOTTERY', subtitle: '一分钟，开启新期待', color: '#e9e2ff', accent: '#8860e9', balls: ['6', '9', '1'], tag: '极速', hot: true, rows: 5, max: 9, zero: true },
  { id: 'k3', name: '快3', english: 'LUCKY DICE', subtitle: '简单有趣 · 乐在其中', color: '#dbf4ed', accent: '#2caf94', balls: ['⚄', '⚂', '⚁'], hot: true, rows: 3, max: 6 },
  { id: 'pk10', name: 'PK10', english: 'RACING CLUB', subtitle: '速度之间 · 感受心跳', color: '#dceeff', accent: '#408dde', balls: ['10', '08', '03'], hot: true, rows: 10, max: 10 },
  { id: 'lhc', name: '澳门六合彩', english: 'MARK SIX', subtitle: '每天21:30 · 六正一特', color: '#fff1ce', accent: '#dfb142', balls: ['6', '28', '9'], hot: false, rows: 6, max: 49 },
  { id: 'pc28', name: 'PC 蛋蛋', english: 'LUCKY TWENTY-EIGHT', subtitle: '幸运相伴 · 快乐加倍', color: '#ffe2e9', accent: '#e97499', balls: ['2', '8', '0'], hot: false, rows: 1, max: 27, zero: true },
]
