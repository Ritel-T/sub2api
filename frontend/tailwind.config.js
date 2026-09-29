// Neutral utility families share the brand palette; status and provider colors stay distinct.
const warmNeutral = {
  50: '#FAF8F5', 100: '#F5EFE6', 200: '#DDD8D0', 300: '#C3BDB4',
  400: '#ABA59B', 500: '#756B5F', 600: '#615D55', 700: '#393833',
  800: '#2E2D29', 900: '#242421', 950: '#1E1E1C'
}
const darkNeutral = { ...warmNeutral, 100: '#F0EDE7', 500: '#8B857B' }

/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{vue,js,ts,jsx,tsx}'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        // 主色调 - 暖中性色
        primary: {
          50: '#F5EFE6',
          100: '#E8E1D6',
          200: '#D2C9BC',
          300: '#C0B5A5',
          400: '#B3AA9E',
          500: '#6F6254',
          600: '#51493E',
          700: '#3F3A32',
          800: '#332F29',
          900: '#292722',
          950: '#1E1E1C'
        },
        brand: { paper: '#F5EFE6', selected: '#F8F4ED' },
        // Common neutral classes and dark surfaces use the same warm palette.
        gray: warmNeutral,
        slate: warmNeutral,
        zinc: warmNeutral,
        neutral: warmNeutral,
        stone: warmNeutral,
        accent: darkNeutral,
        dark: darkNeutral
      },
      fontFamily: {
        sans: [
          'system-ui',
          '-apple-system',
          'BlinkMacSystemFont',
          'Segoe UI',
          'Roboto',
          'Helvetica Neue',
          'Arial',
          'PingFang SC',
          'Hiragino Sans GB',
          'Microsoft YaHei',
          'sans-serif'
        ],
        mono: ['ui-monospace', 'SFMono-Regular', 'Menlo', 'Monaco', 'Consolas', 'monospace']
      },
      boxShadow: {
        glass: '0 8px 32px rgba(0, 0, 0, 0.08)',
        'glass-sm': '0 4px 16px rgba(0, 0, 0, 0.06)',
        glow: '0 0 20px rgba(30, 30, 28, 0.08)',
        'glow-lg': '0 0 40px rgba(30, 30, 28, 0.12)',
        card: '0 1px 3px rgba(0, 0, 0, 0.04), 0 1px 2px rgba(0, 0, 0, 0.06)',
        'card-hover': '0 10px 40px rgba(0, 0, 0, 0.08)',
        'inner-glow': 'inset 0 1px 0 rgba(255, 255, 255, 0.1)'
      },
      backgroundImage: {
        'gradient-radial': 'radial-gradient(var(--tw-gradient-stops))',
        'gradient-primary': 'linear-gradient(135deg, #1E1E1C 0%, #3F3A32 100%)',
        'gradient-dark': 'linear-gradient(135deg, #2E2D29 0%, #1E1E1C 100%)',
        'gradient-glass':
          'linear-gradient(135deg, rgba(255,255,255,0.1) 0%, rgba(255,255,255,0.05) 100%)',
        'mesh-gradient':
          'radial-gradient(at 40% 20%, rgba(166, 158, 148, 0.04) 0px, transparent 50%), radial-gradient(at 80% 0%, rgba(166, 158, 148, 0.025) 0px, transparent 50%), radial-gradient(at 0% 50%, rgba(166, 158, 148, 0.025) 0px, transparent 50%)'
      },
      animation: {
        'fade-in': 'fadeIn 0.3s ease-out',
        'slide-up': 'slideUp 0.3s ease-out',
        'slide-down': 'slideDown 0.3s ease-out',
        'slide-in-right': 'slideInRight 0.3s ease-out',
        'scale-in': 'scaleIn 0.2s ease-out',
        'pulse-slow': 'pulse 3s cubic-bezier(0.4, 0, 0.6, 1) infinite',
        shimmer: 'shimmer 2s linear infinite',
        glow: 'glow 2s ease-in-out infinite alternate'
      },
      keyframes: {
        fadeIn: {
          '0%': { opacity: '0' },
          '100%': { opacity: '1' }
        },
        slideUp: {
          '0%': { opacity: '0', transform: 'translateY(10px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' }
        },
        slideDown: {
          '0%': { opacity: '0', transform: 'translateY(-10px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' }
        },
        slideInRight: {
          '0%': { opacity: '0', transform: 'translateX(20px)' },
          '100%': { opacity: '1', transform: 'translateX(0)' }
        },
        scaleIn: {
          '0%': { opacity: '0', transform: 'scale(0.95)' },
          '100%': { opacity: '1', transform: 'scale(1)' }
        },
        shimmer: {
          '0%': { backgroundPosition: '-200% 0' },
          '100%': { backgroundPosition: '200% 0' }
        },
        glow: {
          '0%': { boxShadow: '0 0 20px rgba(30, 30, 28, 0.08)' },
          '100%': { boxShadow: '0 0 30px rgba(30, 30, 28, 0.12)' }
        }
      },
      backdropBlur: {
        xs: '2px'
      },
      borderRadius: {
        '4xl': '2rem'
      }
    }
  },
  plugins: []
}
