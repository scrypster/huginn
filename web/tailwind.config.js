/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{vue,ts}'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        'huginn-bg':      '#0d1117',
        'huginn-surface': '#161b22',
        'huginn-border':  '#30363d',
        'huginn-text':    '#e6edf3',
        'huginn-muted':   '#8b949e',
        'huginn-blue':    '#58a6ff',
        'huginn-yellow':  '#d29922',
        'huginn-green':   '#3fb950',
        'huginn-red':     '#f85149',
      },
      fontFamily: {
        mono: ['JetBrains Mono', 'Fira Code', 'ui-monospace', 'monospace'],
      },
    },
  },
  plugins: [],
}
