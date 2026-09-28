import { createApp } from 'vue'
import App from './App.vue'
import { createProvider } from './api'
import './fonts.css'
import './style.css'

void createProvider().then(provider => createApp(App, provider).mount('#app'))
