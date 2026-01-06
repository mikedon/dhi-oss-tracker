# SendGrid Email Configuration

This application uses SendGrid for sending email notifications about new DHI adoptions.

## Setup Instructions

### 1. Get SendGrid API Key

1. Sign up for a free SendGrid account at [sendgrid.com](https://sendgrid.com)
2. Navigate to **Settings → API Keys**
3. Click **Create API Key**
4. Give it a name (e.g., "DHI OSS Tracker")
5. Select "Full Access" or at minimum "Mail Send" permissions
6. Copy the API key (you'll only see it once!)

### 2. Configure Environment Variables

Edit your `.env` file and add:

```bash
# Required: Your SendGrid API key
SENDGRID_API_KEY=SG.your_api_key_here

# Required: The "from" email address for notifications
SENDGRID_FROM_EMAIL=noreply@yourdomain.com
```

**Note:** SendGrid requires sender verification. You'll need to:
- Verify a single sender email address, OR
- Verify a domain you own

See [SendGrid Sender Verification](https://docs.sendgrid.com/ui/sending-email/sender-verification)

### 3. Optional Configuration

You can override these defaults in `.env`:

```bash
# SMTP host (default: smtp.sendgrid.net)
SENDGRID_SMTP_HOST=smtp.sendgrid.net

# SMTP port (default: 587)
SENDGRID_SMTP_PORT=587

# SMTP username (default: apikey)
SENDGRID_USERNAME=apikey
```

### 4. Create Email Notifications in UI

1. Open http://localhost:8000
2. Click the **Notifications** tab
3. Click **Add Notification**
4. Fill in:
   - **Name**: A descriptive name (e.g., "Team Email")
   - **Type**: Select "Email"
   - **To Email**: The recipient email address
   - **From Email**: (Optional) Override the default from address
5. Click **Save**
6. Click **Test** to verify it works

## Testing

Test your configuration:

```bash
# Create a test notification
curl -X POST http://localhost:8000/api/notifications \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Test Email",
    "type": "email",
    "enabled": true,
    "config_json": "{\"to\":\"your-email@example.com\"}"
  }'

# Send a test email
curl -X POST http://localhost:8000/api/notifications/1/test
```

## Troubleshooting

### Error: "SENDGRID_API_KEY environment variable is required"

Make sure you've set `SENDGRID_API_KEY` in your `.env` file and restarted the application.

### Email not received

1. Check SendGrid activity log at https://app.sendgrid.com/email_activity
2. Verify your sender email is verified in SendGrid
3. Check spam/junk folder
4. Verify the recipient email is correct

### SendGrid API key invalid

- Make sure you copied the full API key (starts with `SG.`)
- Verify the API key has "Mail Send" permissions
- Try regenerating the API key in SendGrid

## Free Tier Limits

SendGrid free tier includes:
- 100 emails per day
- Basic email analytics
- Single sender verification

This is sufficient for monitoring new DHI adoptions (typically a few emails per week).

## Production Considerations

For production use:
1. Use a verified domain instead of single sender
2. Consider upgrading SendGrid plan for higher volume
3. Set up proper SPF/DKIM/DMARC records for your domain
4. Monitor SendGrid deliverability metrics
