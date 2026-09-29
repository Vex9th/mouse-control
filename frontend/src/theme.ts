import type { GlobalThemeOverrides } from 'naive-ui'

export const mouseTheme: GlobalThemeOverrides = {
  common: {
    fontFamily: '"Mouse UI Sans", "Microsoft YaHei UI", sans-serif',
    fontFamilyMono: '"Mouse UI Sans", "Microsoft YaHei UI", sans-serif',
    fontSize: '14px', fontSizeMini: '12px', fontSizeTiny: '12px', fontSizeSmall: '14px', fontSizeMedium: '14px',
    primaryColor: '#2463eb', primaryColorHover: '#3974f0', primaryColorPressed: '#194fc9', primaryColorSuppl: '#2463eb',
    successColor: '#197a51', warningColor: '#986009', errorColor: '#bf3740', infoColor: '#2463eb',
    textColorBase: '#162335', textColor1: '#162335', textColor2: '#3c4a60', textColor3: '#637186',
    bodyColor: '#f3f5f8', cardColor: '#ffffff', modalColor: '#ffffff', popoverColor: '#ffffff',
    inputColor: '#fbfcfe', tableColor: '#ffffff', actionColor: '#f3f5f8', borderColor: '#d9e0e9', dividerColor: '#edf0f5',
    hoverColor: '#ebf2ff', borderRadius: '6px', borderRadiusSmall: '4px',
    heightMini: '24px', heightTiny: '28px', heightSmall: '32px', heightMedium: '38px', heightLarge: '44px',
  },
  Button: { fontWeight: '500', borderRadiusMedium: '6px', borderRadiusSmall: '6px', textColorPrimary: '#ffffff', textColorHoverPrimary: '#ffffff', textColorPressedPrimary: '#ffffff', textColorFocusPrimary: '#ffffff', colorDisabledPrimary: '#edf1f7', textColorDisabledPrimary: '#64748b', borderDisabledPrimary: '1px solid #d9e0e9', opacityDisabled: '1' },
  Input: { fontSizeSmall: '16px', fontSizeMedium: '17px', borderRadius: '6px', boxShadowFocus: '0 0 0 2px #2463eb22' },
  Radio: { fontSizeSmall: '14px', buttonColorActive: '#ebf2ff', buttonTextColorActive: '#2463eb', buttonBorderColorActive: '#2463eb' },
  Card: { paddingSmall: '16px', titleFontSizeSmall: '16px', titleFontWeight: '600', borderRadius: '9px', boxShadow: 'none' },
  Popover: { padding: '12px', borderRadius: '8px', boxShadow: '0 6px 24px #16233520' },
  Tag: { heightSmall: '22px', fontSizeSmall: '12px', borderRadius: '4px' },
  Pagination: { fontSizeSmall: '12px', itemSizeSmall: '28px' },
  Progress: { fillColor: '#197a51', railColor: '#e3e8f0' },
}
