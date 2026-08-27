import { describe, it, expect } from 'vitest'
import { isTrivialAsk } from '../trivialAsk'

describe('isTrivialAsk', () => {
  it.each([
    'what time is it',
    '@Winston what time is it',
    'time is it',
    'What time is it?',
    'current date',
    '@Winston what day is it?',
    'ping',
    'pong',
    '@Winston ping',
    '@Winston ping!',
    'thanks',
    'thank you',
    'ok',
    'okay',
    'got it',
    'who is here',
    "who's here",
    'who is on the team',
    "who's on the team",
    'roster',
    'how many people are in this channel',
    'who is in this channel',
    "who's in this channel",
    'how many people',
  ])('true: %s', (ask) => {
    expect(isTrivialAsk(ask)).toBe(true)
  })

  it.each([
    'hire Steve',
    'create a teammate',
    'add a teammate',
    'create an agent',
    'create_agent',
    'mesh the hallway',
    '@Winston @Reggie pong',
    'Ask Steve for the hostname',
    'ask Steve',
    'company wall',
    'hello',
    'hello team',
    'list my prs',
  ])('false (full path): %s', (ask) => {
    expect(isTrivialAsk(ask)).toBe(false)
  })
})
