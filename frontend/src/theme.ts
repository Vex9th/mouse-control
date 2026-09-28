import type { GlobalThemeOverrides } from 'naive-ui'

export const mouseTheme: GlobalThemeOverrides = {
  common: {
    fontFamily: '"Mouse UI Sans", "Microsoft YaHei UI", sans-serif',
    fontFamilyMono: '"Mouse UI Sans", "Microsoft YaHei UI", sans-serif',
    fontSize: '14px', fontSizeMini: '12px', fontSizeTiny: '12px', fontSizeSmall: '14px', fontSizeMedium: '14px',
    primaryColor: '#80d8b5', primaryColorHover: '#99e3c5', primaryColorPressed: '#63bb97', primaryColorSuppl: '#80d8b5',
    successColor: '#80d8b5', warningColor: '#efbd79', errorColor: '#f19c98', infoColor: '#a5c5f6',
    textColorBase: '#e6e8eb', textColor1: '#e6e8eb', textColor2: '#c4c9d1', textColor3: '#9fa7b3',
    bodyColor: '#181a1d', cardColor: '#23262b', modalColor: '#23262b', popoverColor: '#292d33',
    inputColor: '#1c1f23', tableColor: '#23262b', actionColor: '#23262b', borderColor: '#3b414a', dividerColor: '#383e46',
    hoverColor: '#30363e', borderRadius: '6px', borderRadiusSmall: '4px',
    heightMini: '24px', heightTiny: '26px', heightSmall: '30px', heightMedium: '34px', heightLarge: '40px',
  },
  Button: { fontWeight: '500', borderRadiusMedium: '6px', borderRadiusSmall: '5px', textColorPrimary: '#15291f', textColorHoverPrimary: '#15291f', textColorPressedPrimary: '#15291f', textColorFocusPrimary: '#15291f', opacityDisabled: '0.6' },
  Input: { fontSizeSmall: '16px', fontSizeMedium: '16px', borderRadius: '5px', boxShadowFocus: '0 0 0 1px #80d8b5' },
  Radio: { fontSizeSmall: '14px' },
  Card: { paddingSmall: '16px', titleFontSizeSmall: '16px', titleFontWeight: '500', borderRadius: '8px', boxShadow: 'none' },
  Popover: { padding: '12px', borderRadius: '7px', boxShadow: '0 4px 16px #0004' },
  Tag: { heightSmall: '22px', fontSizeSmall: '12px', borderRadius: '4px' },
  Pagination: { fontSizeSmall: '12px', itemSizeSmall: '28px' },
  Progress: { fillColor: '#80d8b5', railColor: '#39423f' },
}
