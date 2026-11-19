# Stops an upload built with an SDK older than App Store Connect accepts (ITMS-90725:
# from April 2027, uploads need the iOS 27 SDK, so Xcode 27 or later). Build 118.
require "open3"

MINIMUM_SDK_MAJOR = 27

# "iphoneos27.2" -> 27. nil when it isn't an iPhone SDK name.
def ios_sdk_major(sdk_name)
  match = /\Aiphoneos(\d+)(\.\d+)*\z/.match(sdk_name.to_s.strip)
  match && match[1].to_i
end

# The app's DTSDKName, read from Payload/<App>.app/Info.plist inside the IPA.
def ipa_sdk_name(ipa_path)
  list, status = Open3.capture2("unzip", "-Z1", ipa_path)
  raise "Couldn't read #{ipa_path}" unless status.success?
  plist = list.lines.map(&:strip).find { |line| line.match?(%r{\APayload/[^/]+\.app/Info\.plist\z}) }
  raise "No app Info.plist in #{ipa_path}" unless plist
  xml, status = Open3.capture2("unzip", "-p", ipa_path, plist)
  raise "Couldn't unzip #{plist}" unless status.success?
  name, _, status = Open3.capture3("plutil", "-extract", "DTSDKName", "raw", "-o", "-", "-", stdin_data: xml)
  raise "The app's Info.plist has no DTSDKName" unless status.success?
  name.strip
end

def require_ios_sdk(ipa_path, minimum)
  name = ipa_sdk_name(ipa_path)
  major = ios_sdk_major(name)
  if major.nil? || major < minimum
    raise "Built with #{name}; App Store Connect needs iphoneos#{minimum} or later. Build with Xcode #{minimum}+."
  end
  puts "SDK check: #{name}"
  name
end
