// FRAMEBRIAR_FAKE_SERVICE: test entry only. Production index.html never imports it.
import React from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import { AuthProvider } from '../src/auth/AuthProvider'
import { AppRoutes } from '../src/App'
import { AuthSessionEstablishmentError, type AuthService } from '../src/auth/service'
import '../src/styles/app.css'
const session = async () => {
  const response = await fetch(`/__fixture/session?account=${sessionStorage.getItem('synthetic-account') === 'two' ? 'two' : 'one'}`)
  if (!response.ok) throw new Error('Synthetic session unavailable')
  return response.json()
}
const service: AuthService = {
  getSession: async () => (await session()).user,
  getToken: async expected => {
    const current = await session()
    if (current.user.id !== expected) throw new AuthSessionEstablishmentError()
    return current.token
  },
  signInWithPassword: async () => (await session()).user,
  sendPasswordResetCode: async () => {},
  setPassword: async () => (await session()).user,
  signOut: async () => {},
}
const factory = async () => service
window.history.replaceState({}, '', '/week')
createRoot(document.getElementById('root')!).render(<React.StrictMode><BrowserRouter><AuthProvider serviceFactory={factory}><AppRoutes /></AuthProvider></BrowserRouter></React.StrictMode>)
